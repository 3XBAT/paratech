package audiocapture_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// helperLauncher starts the test binary as the recorder child with the production command line; the n-th launch
// uses scenarios[n] (the last one repeats).
type helperLauncher struct {
	dataDir   string
	scenarios []string

	mu       sync.Mutex
	launches int
}

func (l *helperLauncher) Launch() (audiocapture.Process, error) {
	l.mu.Lock()
	scenario := l.scenarios[min(l.launches, len(l.scenarios)-1)]
	l.launches++
	l.mu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := &audiocapture.CommandLauncher{
		Path: exe, Args: []string{"--recorder", "--data-dir", l.dataDir}, Env: []string{testenv.EnvFakeRecorder + "=" + scenario},
	}
	return cmd.Launch()
}

func (l *helperLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches
}

type alive map[uint32]bool

func (a alive) Alive(pid uint32) (bool, error) { return a[pid], nil }

type recorderEnv struct {
	rec      *audiocapture.Recorder
	launcher *helperLauncher
	store    *audiocapture.TestStore
	chunks   shared.ArtifactRef
	track    shared.ArtifactRef
}

func testConfig() audiocapture.Config {
	cfg := audiocapture.DefaultConfig()
	cfg.ChunkSeconds = 30
	cfg.StartTimeout = 30 * time.Second // a cold start of the race-enabled test binary on a busy CI runner
	cfg.StopGrace = 5 * time.Second
	cfg.HangTimeout = 5 * time.Second // the fake child beats every 50 ms
	cfg.RestartBackoff = 10 * time.Millisecond
	return cfg
}

func newRecorderEnv(t *testing.T, cfg audiocapture.Config, scenarios ...string) *recorderEnv {
	t.Helper()
	dataDir := t.TempDir()
	store := audiocapture.NewTestStore(filepath.Join(dataDir, "artifacts"))
	launcher := &helperLauncher{dataDir: dataDir, scenarios: scenarios}
	rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: launcher, Processes: alive{77: true}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &recorderEnv{rec: rec, launcher: launcher, store: store, chunks: ref(t, "rec1"), track: ref(t, "rec1.wav")}
}

func (e *recorderEnv) diag(t *testing.T) audiocapture.Diagnostics {
	t.Helper()
	d, ok := e.rec.Diagnostics("rec1")
	if !ok {
		t.Fatal("no diagnostics")
	}
	return d
}

func (e *recorderEnv) waitDiag(t *testing.T, what string, cond func(d audiocapture.Diagnostics) bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond(e.diag(t)) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting: %s; diagnostics %+v", what, e.diag(t))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *recorderEnv) names(t *testing.T) []string { return e.store.Names(t, e.chunks) }

func (e *recorderEnv) waitEnded(t *testing.T) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := e.rec.Wait(ctx, "rec1")
	if ctx.Err() != nil {
		t.Fatal("the capture did not end")
	}
	return err
}

func stop(t *testing.T, e *recorderEnv) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return e.rec.Stop(ctx, "rec1")
}

func TestRecorderStartStopFinalize(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderTone)
	ctx := context.Background()

	if err := e.rec.Start(ctx, "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	if err := e.rec.Start(ctx, "rec1", e.chunks); !errors.Is(err, audiocapture.ErrAlreadyCapturing) {
		t.Fatalf("a second Start = %v, want ErrAlreadyCapturing", err)
	}
	// Audio past the first chunk, so the stop closes a second one.
	e.waitDiag(t, "the second chunk has audio", func(d audiocapture.Diagnostics) bool {
		return d.Chunks >= 1 && d.Stats.Frames > 30*int64(testenv.FakeRecorderFormat().SampleRate)
	})
	if err := stop(t, e); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait after Stop = %v", err)
	}

	names := e.names(t)
	if len(names) < 2 || slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, ".partial") }) {
		t.Fatalf("chunks after a clean stop = %v, want closed chunks only", names)
	}
	d, err := e.rec.Finalize(ctx, e.chunks, e.track)
	if err != nil || d <= 30*time.Second {
		t.Fatalf("Finalize = %v, %v", d, err)
	}

	diag := e.diag(t)
	if diag.Running || diag.Mode != audiocapture.ModeExclude || diag.Build != "10.0.19045" || diag.Restarts != 0 ||
		diag.Format != testenv.FakeRecorderFormat() || diag.Stats.Packets == 0 || len(diag.Attempts) != 1 || !diag.Attempts[0].OK {
		t.Fatalf("diagnostics = %+v", diag)
	}
	if diag.Target != (audiocapture.Target{Mode: audiocapture.ModeExclude, PID: uint32(os.Getpid())}) {
		t.Fatalf("default target = %+v, want EXCLUDE of this process", diag.Target)
	}
	if diag.Chunks != len(names) {
		t.Fatalf("diagnostics count %d chunks, the directory has %v", diag.Chunks, names)
	}
}

func TestRecorderCancelledStartKillsTheChild(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderSlowStart)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := e.rec.Start(ctx, "rec1", e.chunks); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start = %v, want the context error", err)
	}
	if _, ok := e.rec.Diagnostics("rec1"); ok {
		t.Fatal("a failed start must leave no capture behind")
	}
	// The recording can start again.
	e.launcher.scenarios = []string{testenv.RecorderTone}
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderStartTimeout(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := testConfig()
	cfg.StartTimeout = 1500 * time.Millisecond
	e := newRecorderEnv(t, cfg, testenv.RecorderSlowStart)
	err := e.rec.Start(context.Background(), "rec1", e.chunks)
	if err == nil || !strings.Contains(err.Error(), "did not start within") {
		t.Fatalf("Start = %v, want the start timeout", err)
	}
}

func TestRecorderStartFailures(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Run("unavailable in this build", func(t *testing.T) {
		e := newRecorderEnv(t, testConfig(), testenv.RecorderUnavailable)
		err := e.rec.Start(context.Background(), "rec1", e.chunks)
		if !errors.Is(err, audiocapture.ErrUnavailable) || !errors.Is(err, recording.ErrCaptureUnavailable) {
			t.Fatalf("Start = %v, want ErrUnavailable", err)
		}
	})
	t.Run("the child cannot run", func(t *testing.T) {
		e := newRecorderEnv(t, testConfig(), "no-such-scenario")
		err := e.rec.Start(context.Background(), "rec1", e.chunks)
		if err == nil || errors.Is(err, recording.ErrCaptureUnavailable) {
			t.Fatalf("Start = %v, want an ordinary failure", err)
		}
	})
	t.Run("the launch fails", func(t *testing.T) {
		rec, err := audiocapture.New(audiocapture.Deps{Files: audiocapture.NewTestStore(t.TempDir()),
			Launcher: &audiocapture.CommandLauncher{Path: filepath.Join(t.TempDir(), "missing")}}, testConfig())
		if err != nil {
			t.Fatal(err)
		}
		if err := rec.Start(context.Background(), "rec1", ref(t, "rec1")); err == nil || errors.Is(err, recording.ErrCaptureUnavailable) {
			t.Fatalf("Start = %v, want an ordinary failure", err)
		}
	})
}

func TestRecorderValidatesTheStart(t *testing.T) {
	e := newRecorderEnv(t, testConfig(), testenv.RecorderTone)
	models, err := shared.NewArtifactRef(shared.ArtifactModels, "x")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		id     shared.RecordingID
		chunks shared.ArtifactRef
		target audiocapture.Target
	}{
		{"bad id", "../x", e.chunks, audiocapture.Target{Mode: audiocapture.ModeSystem}},
		{"zero chunks", "rec1", shared.ArtifactRef{}, audiocapture.Target{Mode: audiocapture.ModeSystem}},
		{"chunks of another kind", "rec1", models, audiocapture.Target{Mode: audiocapture.ModeSystem}},
		{"include without pid", "rec1", e.chunks, audiocapture.Target{Mode: audiocapture.ModeInclude}},
		{"include of a dead process", "rec1", e.chunks, audiocapture.Target{Mode: audiocapture.ModeInclude, PID: 78}},
		{"system with pid", "rec1", e.chunks, audiocapture.Target{Mode: audiocapture.ModeSystem, PID: 5}},
		{"unknown mode", "rec1", e.chunks, audiocapture.Target{Mode: "mic"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := e.rec.StartTarget(context.Background(), tt.id, tt.chunks, tt.target); err == nil {
				t.Fatal("StartTarget accepted it")
			}
		})
	}
	if e.launcher.count() != 0 {
		t.Fatal("an invalid start launched a child")
	}

	noChecker, err := audiocapture.New(audiocapture.Deps{Files: e.store, Launcher: e.launcher}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := noChecker.StartTarget(context.Background(), "rec1", e.chunks, audiocapture.Target{Mode: audiocapture.ModeInclude, PID: 77}); err == nil {
		t.Fatal("INCLUDE without a process checker must be refused")
	}

	ctx := context.Background()
	if err := e.rec.Stop(ctx, "nope"); !errors.Is(err, audiocapture.ErrNotCapturing) {
		t.Fatalf("Stop of an unknown recording = %v", err)
	}
	if err := e.rec.Wait(ctx, "nope"); !errors.Is(err, audiocapture.ErrNotCapturing) {
		t.Fatalf("Wait of an unknown recording = %v", err)
	}
}

func TestRecorderIncludesAChosenApp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderTone)
	if err := e.rec.StartTarget(context.Background(), "rec1", e.chunks, audiocapture.Target{Mode: audiocapture.ModeInclude, PID: 77}); err != nil {
		t.Fatal(err)
	}
	if d := e.diag(t); d.Mode != audiocapture.ModeInclude {
		t.Fatalf("mode = %s", d.Mode)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderRestartsACrashedChild(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderCrash, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	e.waitDiag(t, "the first chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })
	first := e.store.Get(t, ref(t, "rec1/chunk-000001.wav"))

	e.waitDiag(t, "the restarted child closes a chunk", func(d audiocapture.Diagnostics) bool {
		return d.Restarts == 1 && d.Chunks >= 2 && d.Running
	})
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}

	// The crash left its open chunk as .partial; the new child continued after it and overwrote nothing.
	names := e.names(t)
	if len(names) < 3 || names[0] != "chunk-000001.wav" || names[1] != "chunk-000002.wav.partial" || names[2] != "chunk-000003.wav" {
		t.Fatalf("chunks = %v", names)
	}
	if !bytes.Equal(e.store.Get(t, ref(t, "rec1/chunk-000001.wav")), first) {
		t.Fatal("the restart changed an earlier chunk")
	}
	if _, err := e.rec.Finalize(context.Background(), e.chunks, e.track); err != nil {
		t.Fatal(err)
	}
	if d := e.diag(t); !strings.Contains(d.Failure, "exited unexpectedly (code 9)") {
		t.Fatalf("failure = %q", d.Failure)
	}
}

func TestRecorderGivesUpAfterTheRestartBudget(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := testConfig()
	cfg.MaxRestarts = 2
	e := newRecorderEnv(t, cfg, testenv.RecorderCrash)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	err := e.waitEnded(t)
	if !errors.Is(err, audiocapture.ErrCaptureFailed) || !strings.Contains(err.Error(), "no restart left after 2") {
		t.Fatalf("Wait = %v, want ErrCaptureFailed after the budget", err)
	}
	if e.launcher.count() != 3 {
		t.Fatalf("%d children launched, want 3", e.launcher.count())
	}
	if d := e.diag(t); d.Running || d.Restarts != 2 {
		t.Fatalf("diagnostics = %+v", d)
	}
	// Stop after the end changes nothing.
	if err := stop(t, e); err != nil {
		t.Fatalf("Stop after the end = %v", err)
	}
}

func TestRecorderReportsACaptureThatEndedByItself(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderFail)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	err := e.waitEnded(t)
	if !errors.Is(err, audiocapture.ErrCaptureFailed) || !strings.Contains(err.Error(), "device lost") {
		t.Fatalf("Wait = %v", err)
	}
	if e.launcher.count() != 1 {
		t.Fatal("a reported failure must not be restarted")
	}
	if names := e.names(t); !slices.Equal(names, []string{"chunk-000001.wav"}) {
		t.Fatalf("chunks = %v", names)
	}
}

func TestRecorderRestartsAHungChild(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := testConfig()
	cfg.HangTimeout = 2 * time.Second
	e := newRecorderEnv(t, cfg, testenv.RecorderHang, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	e.waitDiag(t, "the hung child is replaced", func(d audiocapture.Diagnostics) bool { return d.Restarts == 1 && d.Running })
	if d := e.diag(t); !strings.Contains(d.Failure, "sent nothing for 2s") {
		t.Fatalf("failure = %q", d.Failure)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderKillsAChildThatDoesNotStop(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := testConfig()
	cfg.StopGrace = 500 * time.Millisecond
	e := newRecorderEnv(t, cfg, testenv.RecorderDeaf)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	err := stop(t, e)
	if err == nil || !strings.Contains(err.Error(), "was killed") {
		t.Fatalf("Stop = %v, want the kill reported", err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait after Stop = %v", err)
	}
	if e.launcher.count() != 1 {
		t.Fatal("a stopped capture must not restart")
	}
}

func TestRecorderStopPastItsContextKills(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderDeaf)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := e.rec.Stop(ctx, "rec1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want the context error", err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}
}

// slowKillLauncher starts in-process children that report a start, ignore their closed stdin and exit only killDelay
// after a kill (a child Windows is slow to end).
type slowKillLauncher struct{ killDelay time.Duration }

func (l slowKillLauncher) Launch() (audiocapture.Process, error) {
	p := &slowKillProcess{killDelay: l.killDelay, exited: make(chan struct{})}
	var inR *io.PipeReader
	inR, p.inW = io.Pipe()
	p.outR, p.outW = io.Pipe()
	p.errR, p.errW = io.Pipe()
	go func() { _, _ = io.Copy(io.Discard, inR) }() // the request, then EOF is ignored
	go func() {
		_, _ = p.outW.Write([]byte(`{"type":"started","mode":"exclude","format":{"rate":8000,"channels":1,"bits":16}}` + "\n"))
	}()
	return p, nil
}

type slowKillProcess struct {
	killDelay  time.Duration
	inW        *io.PipeWriter
	outR, errR *io.PipeReader
	outW, errW *io.PipeWriter
	killOnce   sync.Once
	exited     chan struct{}
}

func (p *slowKillProcess) Stdin() io.WriteCloser { return p.inW }
func (p *slowKillProcess) Stdout() io.Reader     { return p.outR }
func (p *slowKillProcess) Stderr() io.Reader     { return p.errR }
func (p *slowKillProcess) PID() int              { return 1234 }

func (p *slowKillProcess) Kill() error {
	p.killOnce.Do(func() {
		time.AfterFunc(p.killDelay, func() {
			_ = p.outW.Close()
			_ = p.errW.Close()
			close(p.exited)
		})
	})
	return nil
}

func (p *slowKillProcess) Wait() (int, error) {
	<-p.exited
	return 1, nil
}

// The recording session gives a stop 10 s: Stop past its context returns soon even when the killed child lingers,
// and the child is reaped afterwards.
func TestRecorderStopPastItsContextReturnsPromptly(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	if audiocapture.DefaultStopGrace >= 10*time.Second {
		t.Fatalf("the default stop grace %v leaves no time for the kill", audiocapture.DefaultStopGrace)
	}
	rec, err := audiocapture.New(audiocapture.Deps{Files: audiocapture.NewTestStore(t.TempDir()),
		Launcher: slowKillLauncher{killDelay: 4 * time.Second}}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start(context.Background(), "rec1", ref(t, "rec1")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	began := time.Now()
	if err := rec.Stop(ctx, "rec1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want the context error", err)
	}
	if took := time.Since(began); took > 2500*time.Millisecond {
		t.Fatalf("Stop returned %v after its context ended", took-200*time.Millisecond)
	}

	wctx, wcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer wcancel()
	if err := rec.Wait(wctx, "rec1"); err != nil {
		t.Fatalf("Wait = %v, want the end of the stop once the child is reaped", err)
	}
}

// A child that reports a stop nobody asked for is restarted like a crashed one.
func TestRecorderRestartsAChildThatStoppedByItself(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderQuit, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	e.waitDiag(t, "the child is restarted", func(d audiocapture.Diagnostics) bool { return d.Restarts == 1 && d.Running })
	if d := e.diag(t); !strings.Contains(d.Failure, "stopped without a stop request") {
		t.Fatalf("failure = %q", d.Failure)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	if e.launcher.count() != 2 {
		t.Fatalf("%d children launched, want 2", e.launcher.count())
	}
}

func TestRecorderKeepsAFallenBackRecordingInSystemMode(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	crashing := testenv.RecorderFallback + "," + testenv.RecorderCrash
	e := newRecorderEnv(t, testConfig(), crashing, testenv.RecorderFallback)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	e.waitDiag(t, "the restarted child runs", func(d audiocapture.Diagnostics) bool { return d.Restarts == 1 && d.Running })
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}

	// First child: EXCLUDE refused, system ok. The restart opens system mode directly: one more ok attempt only.
	d := e.diag(t)
	modes := make([]string, 0, len(d.Attempts))
	for _, a := range d.Attempts {
		modes = append(modes, string(a.Mode)+map[bool]string{true: ":ok", false: ":failed"}[a.OK])
	}
	if !slices.Equal(modes, []string{"exclude:failed", "system:ok", "system:ok"}) || d.Mode != audiocapture.ModeSystem {
		t.Fatalf("attempts = %v, mode %s", modes, d.Mode)
	}
	if d.Attempts[0].HResult == "" {
		t.Fatal("the HRESULT of the failed attempt is lost")
	}
}

func TestRecorderIgnoresBadLines(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newRecorderEnv(t, testConfig(), testenv.RecorderNoisy)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks); err != nil {
		t.Fatal(err)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if d := e.diag(t); d.InvalidLines != 2 {
		t.Fatalf("invalid lines = %d, want 2", d.InvalidLines)
	}
}

func TestNewValidatesTheConfig(t *testing.T) {
	deps := audiocapture.Deps{Files: audiocapture.NewTestStore(t.TempDir()), Launcher: &helperLauncher{}}
	bad := []func(c *audiocapture.Config){
		func(c *audiocapture.Config) { c.ChunkSeconds = 29 },
		func(c *audiocapture.Config) { c.ChunkSeconds = 61 },
		func(c *audiocapture.Config) { c.StartTimeout = 0 },
		func(c *audiocapture.Config) { c.StopGrace = 0 },
		func(c *audiocapture.Config) { c.HangTimeout = 0 },
		func(c *audiocapture.Config) { c.RestartBackoff = 0 },
		func(c *audiocapture.Config) { c.MaxRestarts = -1 },
	}
	for i, change := range bad {
		cfg := audiocapture.DefaultConfig()
		change(&cfg)
		if _, err := audiocapture.New(deps, cfg); err == nil {
			t.Errorf("case %d: New accepted %+v", i, cfg)
		}
	}
	if _, err := audiocapture.New(audiocapture.Deps{Launcher: deps.Launcher}, audiocapture.DefaultConfig()); err == nil {
		t.Error("New without files")
	}
	if _, err := audiocapture.New(deps, audiocapture.DefaultConfig()); err != nil {
		t.Errorf("default config: %v", err)
	}
}

func TestChildLauncher(t *testing.T) {
	if _, err := audiocapture.ChildLauncher("relative"); err == nil {
		t.Fatal("a relative data directory must be refused")
	}
	dir := t.TempDir()
	l, err := audiocapture.ChildLauncher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Args, []string{"--recorder", "--data-dir", dir}) || len(l.Env) != 0 || !filepath.IsAbs(l.Path) {
		t.Fatalf("launcher = %+v", l)
	}
}
