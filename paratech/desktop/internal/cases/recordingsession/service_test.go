package recordingsession

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

const chunkLen = time.Minute

var errDevice = errors.New("output device changed")

type env struct {
	t     *testing.T
	clock *testenv.FakeClock
	repo  *memRepo
	tr    *memTranscripts
	tx    *fakeTx
	files *testenv.MemArtifacts
	rec   *testenv.FakeRecorder
	svc   *Service
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	e := &env{
		t: t, clock: testenv.NewFakeClock(t0), repo: newMemRepo(), tr: newMemTranscripts(), tx: &fakeTx{},
		files: testenv.NewMemArtifacts(),
	}
	e.rec = testenv.NewFakeRecorder(e.files, chunkLen)
	svc, err := New(Deps{
		Recordings: e.repo, Transcripts: e.tr, Recorder: e.rec, Artifacts: e.files, Tx: e.tx,
		Clock: e.clock, Sleeper: e.clock, IDs: &seqIDs{},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return e
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (e *env) start() Info {
	e.t.Helper()
	info, err := e.svc.Start(testCtx(e.t), StartRequest{})
	if err != nil {
		e.t.Fatalf("Start: %v", err)
	}
	return info
}

func (e *env) feed(id shared.RecordingID, chunks ...string) {
	e.t.Helper()
	for _, c := range chunks {
		if _, err := e.rec.Feed(testCtx(e.t), id, []byte(c)); err != nil {
			e.t.Fatalf("Feed: %v", err)
		}
	}
}

func (e *env) stored(id shared.RecordingID) *recording.Recording {
	e.t.Helper()
	r, err := e.repo.Get(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) awaitStatus(id shared.RecordingID, want recording.Status) *recording.Recording {
	e.t.Helper()
	testenv.Eventually(e.t, "status "+want.String(), func() bool {
		r, err := e.repo.Get(context.Background(), id)
		return err == nil && r.Status() == want
	}, nil)
	_, _ = e.svc.Stop(testCtx(e.t), id) // the session has ended once Stop of a finished recording returns
	testenv.Eventually(e.t, "free slot", func() bool { _, active := e.svc.Active(); return !active }, nil)
	return e.stored(id)
}

func (e *env) content(ref shared.ArtifactRef) string {
	e.t.Helper()
	r, err := e.files.Open(context.Background(), ref)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func requireHistory(t *testing.T, got []recording.Status, want ...recording.Status) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("saved statuses = %v, want %v", got, want)
	}
}

func TestNewValidates(t *testing.T) {
	ok := Deps{
		Recordings: newMemRepo(), Transcripts: newMemTranscripts(), Recorder: testenv.NewFakeRecorder(testenv.NewMemArtifacts(), chunkLen),
		Artifacts: testenv.NewMemArtifacts(), Tx: &fakeTx{}, Clock: testenv.NewFakeClock(t0), Sleeper: testenv.NewFakeClock(t0),
		IDs: &seqIDs{},
	}
	cases := []struct {
		name string
		deps func(d Deps) Deps
		cfg  func(c Config) Config
	}{
		{"no repository", func(d Deps) Deps { d.Recordings = nil; return d }, nil},
		{"no transcripts", func(d Deps) Deps { d.Transcripts = nil; return d }, nil},
		{"no recorder", func(d Deps) Deps { d.Recorder = nil; return d }, nil},
		{"no artifacts", func(d Deps) Deps { d.Artifacts = nil; return d }, nil},
		{"no tx", func(d Deps) Deps { d.Tx = nil; return d }, nil},
		{"no clock", func(d Deps) Deps { d.Clock = nil; return d }, nil},
		{"no sleeper", func(d Deps) Deps { d.Sleeper = nil; return d }, nil},
		{"no ids", func(d Deps) Deps { d.IDs = nil; return d }, nil},
		{"zero cap", nil, func(c Config) Config { c.MaxDuration = 0; return c }},
		{"cap too long", nil, func(c Config) Config { c.MaxDuration = 13 * time.Hour; return c }},
		{"no extension", nil, func(c Config) Config { c.TrackExtension = ""; return c }},
		{"bad extension", nil, func(c Config) Config { c.TrackExtension = "../x"; return c }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, c := ok, DefaultConfig()
			if tc.deps != nil {
				d = tc.deps(d)
			}
			if tc.cfg != nil {
				c = tc.cfg(c)
			}
			if _, err := New(d, c); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New = %v, want ErrInvalidConfig", err)
			}
		})
	}

	svc, err := New(ok, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// AC-03: a recording is started and stopped through the Core API and stored locally.
func TestStartStopCompletes(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())

	info, err := e.svc.Start(testCtx(t), StartRequest{LessonID: "lesson-1"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != recording.StatusRecording || info.ID != "rec-1" || info.LessonID != "lesson-1" ||
		!info.Deadline.Equal(t0.Add(limits.DefaultMaxDuration)) {
		t.Fatalf("Start = %+v", info)
	}
	if active, ok := e.svc.Active(); !ok || active.ID != info.ID || active.Status != recording.StatusRecording {
		t.Fatalf("Active = %+v %v", active, ok)
	}
	if !e.rec.Running(info.ID) {
		t.Fatal("capture does not run")
	}

	e.feed(info.ID, "aa", "bb", "cc")
	e.clock.Advance(3 * chunkLen)
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != recording.StatusCompleted || got.Chunks != 3 || got.Duration != 3*chunkLen || got.Problem != "" ||
		!got.StoppedAt.Equal(t0.Add(3*chunkLen)) {
		t.Fatalf("Stop = %+v", got)
	}

	r := e.stored(info.ID)
	if r.Audio().String() != "recordings/rec-1.wav" || e.content(r.Audio()) != "aabbcc" {
		t.Fatalf("final track %s", r.Audio())
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording, recording.StatusStopping,
		recording.StatusFinalizing, recording.StatusCompleted)
	if e.rec.Running(info.ID) {
		t.Fatal("capture still runs")
	}
	if _, ok := e.svc.Active(); ok {
		t.Fatal("a finished recording is still active")
	}
	if want := []string{"recordings/rec-1.wav", "recordings/rec-1/chunk-0001", "recordings/rec-1/chunk-0002", "recordings/rec-1/chunk-0003"}; !slices.Equal(e.files.Paths(), want) {
		t.Fatalf("files = %v, want %v", e.files.Paths(), want)
	}
}

// AC-04: the hard cap finalizes the recording without a stop.
func TestHardCapFinalizes(t *testing.T) {
	cases := []struct {
		name string
		cap  time.Duration
	}{
		{"default 3h", 0},
		{"configured 90m", 90 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			cfg := DefaultConfig()
			if tc.cap != 0 {
				cfg.MaxDuration = tc.cap
			}
			e := newEnv(t, cfg)
			info := e.start()
			e.feed(info.ID, "a", "b")
			ctx := testCtx(t)
			if err := e.clock.AwaitSleepers(ctx, 1); err != nil {
				t.Fatal(err)
			}

			e.clock.Advance(cfg.MaxDuration - time.Second)
			if e.clock.Sleepers() != 1 || !e.rec.Running(info.ID) {
				t.Fatal("the cap fired early")
			}

			e.clock.Advance(time.Second)
			r := e.awaitStatus(info.ID, recording.StatusCompleted)
			requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording,
				recording.StatusFinalizing, recording.StatusCompleted)
			if !r.StoppedAt().Equal(t0.Add(cfg.MaxDuration)) || r.Chunks() != 2 || e.content(r.Audio()) != "ab" {
				t.Fatalf("finalized: stopped %v, chunks %d", r.StoppedAt(), r.Chunks())
			}
			if e.rec.Running(info.ID) {
				t.Fatal("capture still runs after the cap")
			}
		})
	}
}

func TestCaptureErrorFinalizesWhatWasRecorded(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a", "b")

	e.rec.End(info.ID, errDevice)
	r := e.awaitStatus(info.ID, recording.StatusCompleted)
	if !strings.Contains(r.Problem(), problemCaptureEnded) || !strings.Contains(r.Problem(), errDevice.Error()) {
		t.Fatalf("problem = %q", r.Problem())
	}
	if e.content(r.Audio()) != "ab" {
		t.Fatal("the recorded chunks were not assembled")
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording,
		recording.StatusFinalizing, recording.StatusCompleted)
}

func TestDiskErrorFailsAndKeepsChunks(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a", "b")

	e.files.FailWrites(shared.ErrStorageFull)
	if _, err := e.rec.Feed(testCtx(t), info.ID, []byte("c")); !errors.Is(err, shared.ErrStorageFull) {
		t.Fatalf("Feed on a full disk = %v", err)
	}
	r := e.awaitStatus(info.ID, recording.StatusFailed)
	if !strings.Contains(r.Problem(), problemFinalizeFailed) || r.Chunks() != 2 || !r.Audio().IsZero() {
		t.Fatalf("failed recording: problem %q, chunks %d, audio %s", r.Problem(), r.Chunks(), r.Audio())
	}
	if want := []string{"recordings/rec-1/chunk-0001", "recordings/rec-1/chunk-0002"}; !slices.Equal(e.files.Paths(), want) {
		t.Fatalf("files = %v, want the chunks kept", e.files.Paths())
	}
}

func TestFinalizeErrorAfterStopFails(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")
	e.rec.FailFinalize(shared.ErrStoragePermission)

	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusFailed || got.Chunks != 1 {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording, recording.StatusStopping,
		recording.StatusFinalizing, recording.StatusFailed)
	if len(e.files.Paths()) != 1 {
		t.Fatalf("files = %v, want the chunk kept", e.files.Paths())
	}
}

func TestStopWithoutAudioFails(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusFailed || got.Chunks != 0 {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

func TestRecorderStopErrorStillAssembles(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")
	e.rec.FailStop(errDevice)

	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

func TestStartErrorFails(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.rec.FailStart(errDevice)

	info, err := e.svc.Start(testCtx(t), StartRequest{})
	if !errors.Is(err, ErrStartFailed) || !errors.Is(err, errDevice) || info.Status != recording.StatusFailed {
		t.Fatalf("Start = %+v, %v", info, err)
	}
	r := e.stored(info.ID)
	if r.Status() != recording.StatusFailed || !strings.Contains(r.Problem(), problemStartFailed) {
		t.Fatalf("stored: %s %q", r.Status(), r.Problem())
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusFailed)

	e.rec.FailStart(nil)
	if next := e.start(); next.ID != "rec-2" {
		t.Fatalf("next start = %+v", next)
	}
}

func TestStartInsertErrorLeavesNothing(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.repo.failNextSaves(shared.ErrStorageBusy)
	if _, err := e.svc.Start(testCtx(t), StartRequest{}); !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Start = %v", err)
	}
	if e.repo.has("rec-1") || e.rec.Starts() != 0 {
		t.Fatal("a recording was created or started")
	}
	if _, ok := e.svc.Active(); ok {
		t.Fatal("the slot was not freed")
	}
}

func TestStopDuringStartAbortsTheStart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	entered, release := e.rec.HoldStart()
	defer release()

	type result struct {
		info Info
		err  error
	}
	started := make(chan result, 1)
	go func() {
		info, err := e.svc.Start(testCtx(t), StartRequest{})
		started <- result{info, err}
	}()
	<-entered

	got, err := e.svc.Stop(testCtx(t), "rec-1")
	if err != nil || got.Status != recording.StatusFailed || got.Problem != problemStartAborted {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	res := <-started
	if !errors.Is(res.err, ErrStartAborted) || res.info.Status != recording.StatusFailed {
		t.Fatalf("Start = %+v, %v", res.info, res.err)
	}
	if e.rec.Running("rec-1") {
		t.Fatal("capture runs after an aborted start")
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusFailed)
}

func TestCallerCancelDuringStartAbortsTheStart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	entered, release := e.rec.HoldStart()
	defer release()

	ctx, cancel := context.WithCancel(testCtx(t))
	errc := make(chan error, 1)
	go func() {
		_, err := e.svc.Start(ctx, StartRequest{})
		errc <- err
	}()
	<-entered
	cancel()
	if err := <-errc; !errors.Is(err, ErrStartAborted) {
		t.Fatalf("Start = %v", err)
	}
	if r := e.stored("rec-1"); r.Status() != recording.StatusFailed {
		t.Fatalf("stored status %s", r.Status())
	}
}

// A stop that arrives while the recorder finishes its start: the capture already runs, so it is stopped normally.
func TestStopAsTheCaptureStarts(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	e.rec.OnStarted(func(id shared.RecordingID) {
		e.feed(id, "a")
		e.svc.mu.Lock()
		sess := e.svc.active
		e.svc.mu.Unlock()
		sess.requestStop()
	})

	info, err := e.svc.Start(testCtx(t), StartRequest{})
	if err != nil {
		t.Fatalf("Start = %v", err)
	}
	got, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || got.Status != recording.StatusCompleted || got.Chunks != 1 {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")
	first, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.Stop(testCtx(t), info.ID)
	if err != nil || again.Status != recording.StatusCompleted || again.Duration != first.Duration {
		t.Fatalf("second Stop = %+v, %v", again, err)
	}
	if n := len(e.repo.statuses()); n != 5 {
		t.Fatalf("a second Stop saved again: %d saves", n)
	}

	if _, err := e.svc.Stop(testCtx(t), "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Stop of an unknown recording = %v", err)
	}

	orphan, err := recording.New(recording.Spec{ID: "orphan", MaxDuration: time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := orphan.Begin(t0); err != nil {
		t.Fatal(err)
	}
	e.repo.put(orphan)
	if got, err := e.svc.Stop(testCtx(t), "orphan"); !errors.Is(err, ErrNotRunning) || got.Status != recording.StatusRecording {
		t.Fatalf("Stop of an interrupted recording = %+v, %v", got, err)
	}
}

func TestConcurrentStartsAdmitOne(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok []Info
	refused := 0
	for range n {
		wg.Go(func() {
			info, err := e.svc.Start(testCtx(t), StartRequest{})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok = append(ok, info)
			case errors.Is(err, limits.ErrRecordingInProgress):
				refused++
			default:
				t.Errorf("Start = %v", err)
			}
		})
	}
	wg.Wait()
	if len(ok) != 1 || refused != n-1 {
		t.Fatalf("%d starts succeeded, %d refused", len(ok), refused)
	}
	if e.rec.Starts() != 1 {
		t.Fatalf("recorder started %d times", e.rec.Starts())
	}

	if _, err := e.svc.Start(testCtx(t), StartRequest{}); !errors.Is(err, limits.ErrRecordingInProgress) {
		t.Fatalf("Start while recording = %v", err)
	}
	e.feed(ok[0].ID, "a")
	if _, err := e.svc.Stop(testCtx(t), ok[0].ID); err != nil {
		t.Fatal(err)
	}
	if next := e.start(); next.Status != recording.StatusRecording {
		t.Fatalf("Start after the stop = %+v", next)
	}
}

func TestCloseFinalizesTheActiveRecording(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")

	if err := e.svc.Close(testCtx(t)); err != nil {
		t.Fatal(err)
	}
	if r := e.stored(info.ID); r.Status() != recording.StatusCompleted {
		t.Fatalf("after Close: %s", r.Status())
	}
	if _, err := e.svc.Start(testCtx(t), StartRequest{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close = %v", err)
	}
}

func TestForcedCloseLeavesTheStateForRecovery(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")
	entered, release := e.rec.HoldFinalize()
	defer release()

	ctx, cancel := context.WithCancel(testCtx(t))
	closed := make(chan error, 1)
	go func() { closed <- e.svc.Close(ctx) }()
	<-entered
	cancel()

	if err := <-closed; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close = %v", err)
	}
	if r := e.stored(info.ID); r.Status() != recording.StatusFinalizing {
		t.Fatalf("after a forced Close: %s, want FINALIZING for the recovery", r.Status())
	}
	if e.rec.Running(info.ID) {
		t.Fatal("capture still runs")
	}
}

func TestShutdownWhileRecordingAbandons(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()

	e.svc.shutdown() // what a forced Close does after its context ended
	testenv.Eventually(t, "session end", func() bool { _, active := e.svc.Active(); return !active }, nil)
	if r := e.stored(info.ID); r.Status() != recording.StatusRecording {
		t.Fatalf("after the shutdown: %s, want RECORDING for the recovery", r.Status())
	}
	if e.rec.Running(info.ID) {
		t.Fatal("capture still runs")
	}
	if _, err := e.svc.Stop(testCtx(t), info.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop after the shutdown = %v", err)
	}
}

func TestLostOwnershipStopsTheSession(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()

	other := e.stored(info.ID) // a recovery of another process takes the recording over
	if err := other.RequireRecovery(t0.Add(time.Minute), "stale heartbeat"); err != nil {
		t.Fatal(err)
	}
	e.repo.put(other)

	got, err := e.svc.Stop(testCtx(t), info.ID)
	if !errors.Is(err, ErrAbandoned) || !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	if e.rec.Running(info.ID) {
		t.Fatal("capture still runs")
	}
	if r := e.stored(info.ID); r.Status() != recording.StatusRecoveryRequired {
		t.Fatalf("the session overwrote the other writer: %s", r.Status())
	}
}

func TestSaveErrorsAreRetriedOrCarriedOver(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")

	// STOPPING and FINALIZING are lost, COMPLETED fails twice and then succeeds after the retry delays.
	e.repo.failNextSaves(shared.ErrStorageBusy, shared.ErrStorageBusy, shared.ErrStorageBusy, shared.ErrStorageBusy)
	stopped := make(chan error, 1)
	go func() {
		_, err := e.svc.Stop(testCtx(t), info.ID)
		stopped <- err
	}()
	testenv.Eventually(t, "final save", func() bool { return e.repo.statusOf(info.ID) == recording.StatusCompleted },
		func() { e.clock.Advance(100 * time.Millisecond) })
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	requireHistory(t, e.repo.statuses(), recording.StatusStarting, recording.StatusRecording, recording.StatusCompleted)
}

func TestFinalSaveGivesUp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	info := e.start()
	e.feed(info.ID, "a")

	errs := make([]error, 2+finalSaveAttempts)
	for i := range errs {
		errs[i] = shared.ErrStorageBusy
	}
	e.repo.failNextSaves(errs...)
	stopped := make(chan error, 1)
	go func() {
		_, err := e.svc.Stop(testCtx(t), info.ID)
		stopped <- err
	}()
	var err error
	testenv.Eventually(t, "Stop", func() bool {
		select {
		case err = <-stopped:
			return true
		default:
			return false
		}
	}, func() { e.clock.Advance(time.Second) })
	if !errors.Is(err, ErrAbandoned) || !errors.Is(err, shared.ErrStorageBusy) {
		t.Fatalf("Stop = %v", err)
	}
	if r := e.stored(info.ID); r.Status() != recording.StatusRecording {
		t.Fatalf("stored %s, want the last saved state for the recovery", r.Status())
	}
}

func TestGetAndActive(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := newEnv(t, DefaultConfig())
	if _, ok := e.svc.Active(); ok {
		t.Fatal("active before a start")
	}
	info := e.start()
	got, err := e.svc.Get(testCtx(t), info.ID)
	if err != nil || got != info {
		t.Fatalf("Get = %+v, %v; want %+v", got, err, info)
	}
	if _, err := e.svc.Get(testCtx(t), "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
}
