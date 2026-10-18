package audiocapture_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

// levelLine is a level check request of d for target.
func levelLine(target audiocapture.Target, d time.Duration) string {
	return fmt.Sprintf(`{"v":1,"kind":"levelcheck","target":{"mode":%q,"pid":%d},"duration_ms":%d}`, target.Mode, target.PID,
		d.Milliseconds())
}

// levelsOf returns the levels of the level events.
func levelsOf(t *testing.T, run *childRun) []float64 {
	t.Helper()
	var out []float64
	for _, ev := range run.out.find(t, "level") {
		out = append(out, ev["level"].(float64))
	}
	return out
}

func source(mod func(s *testenv.ToneSource)) *testenv.ToneSource {
	s := &testenv.ToneSource{Format: s16, PacketFrames: 80, Interval: 2 * time.Millisecond}
	if mod != nil {
		mod(s)
	}
	return s
}

// The level check child (TASK-075) on the fake source: level events at most every LevelInterval, the stream always
// closed, a last "stopped" or "failed" with its cause, and no file written. It ends by itself after its duration:
// stdin stays open here.
func TestChildLevelCheck(t *testing.T) {
	include := audiocapture.Target{Mode: audiocapture.ModeInclude, PID: 300}
	tests := []struct {
		name    string
		src     *testenv.ToneSource
		target  audiocapture.Target
		code    int
		last    string // type of the last event
		cause   string // of a failed end
		heard   bool
		levels  bool // level events expected
		streams int  // closed streams
	}{
		{name: "the source plays", src: source(nil), target: include, code: audiocapture.ChildExitOK, last: "stopped",
			heard: true, levels: true, streams: 1},
		{name: "silence", src: source(func(s *testenv.ToneSource) { s.Gate = func() bool { return false } }), target: include,
			code: audiocapture.ChildExitOK, last: "stopped", levels: true, streams: 1},
		{name: "silent packets", src: source(func(s *testenv.ToneSource) { s.Gate = func() bool { return false }; s.SilentAt = 2 }),
			target: include, code: audiocapture.ChildExitOK, last: "stopped", levels: true, streams: 1},
		{name: "whole computer falls back to classic loopback",
			src:    source(func(s *testenv.ToneSource) { s.FallbackTo = audiocapture.ModeSystem }),
			target: audiocapture.Target{Mode: audiocapture.ModeExclude, PID: 4242}, code: audiocapture.ChildExitOK,
			last: "stopped", heard: true, levels: true, streams: 1},
		{name: "no capture in this build", src: source(func(s *testenv.ToneSource) { s.OpenErr = audiocapture.ErrUnavailable }),
			target: include, code: audiocapture.ChildExitFailed, last: "failed", cause: "unsupported"},
		{name: "no output device", src: source(func(s *testenv.ToneSource) { s.OpenErr = audiocapture.ErrNoDevice }),
			target: include, code: audiocapture.ChildExitFailed, last: "failed", cause: "no_device"},
		{name: "the device fails", src: source(func(s *testenv.ToneSource) { s.OpenErr = audiocapture.ErrDevice }),
			target: include, code: audiocapture.ChildExitFailed, last: "failed", cause: "failed"},
		{name: "the capture ends by itself", src: source(func(s *testenv.ToneSource) { s.FailAfter = 3 }), target: include,
			code: audiocapture.ChildExitFailed, last: "failed", cause: "failed", heard: true, streams: 1},
		{name: "the open never completes", src: source(func(s *testenv.ToneSource) { s.OpenBlock = true }), target: include,
			code: audiocapture.ChildExitOK, last: "stopped"},
		{name: "the stream hangs", src: source(func(s *testenv.ToneSource) { s.HangAfter = 5 }), target: include,
			code: audiocapture.ChildExitOK, last: "stopped", heard: true, levels: true, streams: 1},
	}
	const d = 600 * time.Millisecond
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			start := time.Now()
			run := startChild(t, context.Background(), tt.src, levelLine(tt.target, d), audiocapture.ChildConfig{})
			if code := run.exit(t); code != tt.code {
				t.Fatalf("exit code %d, want %d; events %v", code, tt.code, run.out.events(t))
			}
			if took := time.Since(start); took > d+5*time.Second {
				t.Fatalf("the check took %v for %v", took, d)
			}

			events := run.out.events(t)
			last := events[len(events)-1]
			if last["type"] != tt.last || (tt.cause != "" && last["cause"] != tt.cause) {
				t.Fatalf("last event %v, want %s %s", last, tt.last, tt.cause)
			}
			levels := levelsOf(t, run)
			if tt.levels && len(levels) == 0 {
				t.Fatal("no level reported")
			}
			// At most 20 a second: d / 50 ms and the edges.
			if limit := int(d/audiocapture.DefaultLevelInterval) + 2; len(levels) > limit {
				t.Fatalf("%d levels in %v, want at most %d", len(levels), d, limit)
			}
			heard := slices.ContainsFunc(levels, func(l float64) bool { return values.SoundLevel(l).Heard() })
			if tt.levels && heard != tt.heard {
				t.Fatalf("levels %v, heard %v, want %v", levels, heard, tt.heard)
			}
			for _, l := range levels {
				if l < 0 || l > 1 {
					t.Fatalf("level %v out of range", l)
				}
			}
			if opened := tt.src.Opened(); len(opened) != 1 || opened[0] != tt.target {
				t.Fatalf("opened %v, want one capture of %v", opened, tt.target)
			}
			if tt.src.Closes() != tt.streams {
				t.Fatalf("%d streams closed, want %d", tt.src.Closes(), tt.streams)
			}
			assertNoFiles(t, run.store)
		})
	}
}

// The levels of a tone at half of full scale are about 0.5; the fake source of the recorder child plays it.
func TestChildLevelCheckMeasuresThePeak(t *testing.T) {
	run := startChild(t, context.Background(), source(nil), levelLine(audiocapture.Target{Mode: audiocapture.ModeSystem},
		300*time.Millisecond), audiocapture.ChildConfig{})
	if code := run.exit(t); code != audiocapture.ChildExitOK {
		t.Fatalf("exit code %d", code)
	}
	if peak := slices.Max(levelsOf(t, run)); peak < 0.45 || peak > 0.55 {
		t.Fatalf("peak %v, want about 0.5", peak)
	}
}

// A closed stdin or an ended ctx ends a long check early, the stream closed and "stopped" reported.
func TestChildLevelCheckStops(t *testing.T) {
	for _, how := range []string{"stdin", "ctx"} {
		t.Run(how, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			src := source(nil)
			run := startChild(t, ctx, src, levelLine(audiocapture.Target{Mode: audiocapture.ModeSystem}, time.Minute),
				audiocapture.ChildConfig{})
			testenv.Eventually(t, "a level is reported", func() bool { return len(run.out.find(t, "level")) > 0 }, nil)
			start := time.Now()
			if how == "stdin" {
				_ = run.stdin.Close()
			} else {
				cancel()
			}
			if code := run.exit(t); code != audiocapture.ChildExitOK || time.Since(start) > 5*time.Second {
				t.Fatalf("exit code %d after %v", code, time.Since(start))
			}
			events := run.out.events(t)
			if events[len(events)-1]["type"] != "stopped" || src.Closes() != 1 {
				t.Fatalf("last event %v, %d streams closed", events[len(events)-1], src.Closes())
			}
			assertNoFiles(t, run.store)
		})
	}
}

func assertNoFiles(t *testing.T, store *audiocapture.TestStore) {
	t.Helper()
	err := filepath.WalkDir(store.Root(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("the level check wrote %s", path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func checkEnv(t *testing.T, grace time.Duration, scenarios ...string) *recorderEnv {
	t.Helper()
	cfg := testConfig()
	cfg.CheckGrace = grace
	return newRecorderEnv(t, cfg, scenarios...)
}

// collect gathers the reported levels.
type collect struct {
	mu     sync.Mutex
	levels []values.SoundLevel
}

func (c *collect) add(l values.SoundLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.levels = append(c.levels, l)
}

func (c *collect) all() []values.SoundLevel {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.levels)
}

func heardAny(levels []values.SoundLevel) bool {
	return slices.ContainsFunc(levels, values.SoundLevel.Heard)
}

// CheckLevels runs the level check in a recorder child launched like the production one (the test binary as the fake
// child), forwards the valid levels and maps every end of it.
func TestRecorderCheckLevels(t *testing.T) {
	tests := []struct {
		scenario string
		end      values.SoundCause
		heard    bool
		levels   bool
	}{
		{scenario: testenv.RecorderTone, end: values.SoundOK, heard: true, levels: true},
		{scenario: testenv.RecorderSilent, end: values.SoundOK, levels: true},
		{scenario: testenv.RecorderFallback, end: values.SoundOK, heard: true, levels: true},
		{scenario: testenv.RecorderBadLevels, end: values.SoundOK, heard: true, levels: true},
		{scenario: testenv.RecorderSlowStart, end: values.SoundOK},
		{scenario: testenv.RecorderUnavailable, end: values.SoundUnsupported},
		{scenario: testenv.RecorderNoDevice, end: values.SoundNoDevice},
		{scenario: testenv.RecorderFail, end: values.SoundFailed, heard: true},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := checkEnv(t, 30*time.Second, tt.scenario)
			var got collect
			end, err := e.rec.CheckLevels(context.Background(), values.WholeComputer(), 700*time.Millisecond, got.add)
			if err != nil || end != tt.end {
				t.Fatalf("CheckLevels = %s, %v; want %s", end, err, tt.end)
			}
			levels := got.all()
			if tt.levels && len(levels) == 0 {
				t.Fatal("no level forwarded")
			}
			if tt.levels && heardAny(levels) != tt.heard {
				t.Fatalf("levels %v, heard %v, want %v", levels, heardAny(levels), tt.heard)
			}
			for _, l := range levels {
				if l < 0 || l > 1 {
					t.Fatalf("level %v forwarded", l)
				}
			}
			assertNoFiles(t, e.store)
		})
	}
}

// An app is captured with INCLUDE of the process Deps.Apps finds; one that does not run is refused without a child.
func TestRecorderCheckLevelsOfAnApp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	finder := newFakeFinder(zoom1)
	e, logs := newAppEnv(t, testConfig(), finder, testenv.RecorderTone)
	end, err := e.rec.CheckLevels(context.Background(), zoomSource(t), 300*time.Millisecond, nil)
	if err != nil || end != values.SoundOK {
		t.Fatalf("CheckLevels = %s, %v", end, err)
	}
	if logs.count("sound check child launched") != 1 || !lineHas(logs.String(), "sound check child launched", "target=include") {
		t.Fatalf("log %s", logs)
	}

	finder.set()
	end, err = e.rec.CheckLevels(context.Background(), zoomSource(t), 300*time.Millisecond, nil)
	if !errors.Is(err, recording.ErrSourceNotRunning) || end != "" || e.launcher.count() != 1 {
		t.Fatalf("an app that does not run: %s, %v; %d launches", end, err, e.launcher.count())
	}

	// A build that cannot find apps refuses them.
	noApps, _ := newAppEnv(t, testConfig(), nil, testenv.RecorderTone)
	if _, err := noApps.rec.CheckLevels(context.Background(), zoomSource(t), time.Second, nil); !errors.Is(err, audiocapture.ErrInvalidTarget) {
		t.Fatalf("no app finder: %v", err)
	}

	// The whole computer is EXCLUDE of this app, as a recording.
	whole, wholeLogs := newAppEnv(t, testConfig(), nil, testenv.RecorderTone)
	if end, err := whole.rec.CheckLevels(context.Background(), values.WholeComputer(), 300*time.Millisecond, nil); err != nil ||
		end != values.SoundOK || !lineHas(wholeLogs.String(), "sound check child launched", "target=exclude") {
		t.Fatalf("whole computer: %s, %v\n%s", end, err, wholeLogs)
	}
}

// lineHas reports whether a log line with msg contains s.
func lineHas(log, msg, s string) bool {
	line := lineOf(log, msg)
	return line != "" && strings.Contains(line, s)
}

func TestRecorderCheckLevelsValidatesTheDuration(t *testing.T) {
	e := checkEnv(t, time.Second, testenv.RecorderTone)
	for _, d := range []time.Duration{0, -time.Second, time.Microsecond, audiocapture.MaxCheckDuration + time.Second} {
		if _, err := e.rec.CheckLevels(context.Background(), values.WholeComputer(), d, nil); err == nil {
			t.Errorf("duration %v accepted", d)
		}
	}
	if e.launcher.count() != 0 {
		t.Fatalf("%d launches", e.launcher.count())
	}
}

// A child that never answers is killed past the duration and the grace; a second check meanwhile is refused.
func TestRecorderCheckLevelsTimesOut(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := checkEnv(t, time.Second, testenv.RecorderHang)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := e.rec.CheckLevels(context.Background(), values.WholeComputer(), 500*time.Millisecond, nil)
		done <- err
	}()
	testenv.Eventually(t, "the child is launched", func() bool { return e.launcher.count() == 1 }, nil)
	if _, err := e.rec.CheckLevels(context.Background(), values.WholeComputer(), time.Second, nil); !errors.Is(err, audiocapture.ErrSelfTestRunning) {
		t.Fatalf("a second check = %v, want ErrSelfTestRunning", err)
	}
	select {
	case err := <-done:
		if err == nil || time.Since(start) > 30*time.Second {
			t.Fatalf("CheckLevels = %v after %v, want the timeout", err, time.Since(start))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("CheckLevels did not return after the timeout")
	}
	if e.launcher.count() != 1 {
		t.Fatalf("%d launches", e.launcher.count())
	}
}

// Cancelling the caller's ctx (the user stops the check) kills the child at once; CheckLevels returns the cause.
func TestRecorderCheckLevelsCancelled(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := checkEnv(t, 30*time.Second, testenv.RecorderTone)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		_, err := e.rec.CheckLevels(ctx, values.WholeComputer(), 50*time.Second, func(values.SoundLevel) {
			once.Do(func() { close(first) })
		})
		done <- err
	}()
	select {
	case <-first:
	case <-time.After(30 * time.Second):
		t.Fatal("no level")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || time.Since(start) > 10*time.Second {
			t.Fatalf("CheckLevels = %v after %v", err, time.Since(start))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("CheckLevels did not return after the cancel")
	}
}

// While a recording captures, the check is refused without a child: it never disturbs the lecture.
func TestRecorderCheckLevelsRefusedWhileCapturing(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := checkEnv(t, 30*time.Second, testenv.RecorderTone)
	ctx := context.Background()
	if err := e.rec.Start(ctx, "rec1", e.chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	end, err := e.rec.CheckLevels(ctx, values.WholeComputer(), time.Second, nil)
	if err != nil || end != values.SoundBusy || e.launcher.count() != 1 {
		t.Fatalf("CheckLevels while capturing = %s, %v; %d launches", end, err, e.launcher.count())
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if end, err := e.rec.CheckLevels(ctx, values.WholeComputer(), 300*time.Millisecond, nil); err != nil || end != values.SoundOK {
		t.Fatalf("CheckLevels after the stop = %s, %v", end, err)
	}
}

// A recording that starts during a check stops it before its own child starts; the check answers busy.
func TestRecorderStartStopsTheSoundCheck(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dataDir := t.TempDir()
	logs := &logBuffer{}
	inner := &helperLauncher{dataDir: dataDir, scenarios: []string{testenv.RecorderTone}}
	launcher := &stopOrderLauncher{helperLauncher: inner, logs: logs}
	cfg := testConfig()
	cfg.CheckGrace = 30 * time.Second
	store := audiocapture.NewTestStore(filepath.Join(dataDir, "artifacts"))
	rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: launcher, Processes: alive{77: true},
		Log: slog.New(slog.NewTextHandler(logs, nil))}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := &recorderEnv{rec: rec, launcher: inner, store: store, chunks: ref(t, "rec1"), track: ref(t, "rec1.wav")}

	type answer struct {
		end values.SoundCause
		err error
	}
	done := make(chan answer, 1)
	var got collect
	go func() {
		end, err := e.rec.CheckLevels(context.Background(), values.WholeComputer(), 50*time.Second, got.add)
		done <- answer{end, err}
	}()
	testenv.Eventually(t, "the check reports levels", func() bool { return len(got.all()) > 0 }, nil)

	if err := e.rec.Start(context.Background(), "rec1", e.chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-done:
		if a.err != nil || a.end != values.SoundBusy {
			t.Fatalf("CheckLevels stopped by a recording = %s, %v; want busy", a.end, a.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the check still runs after the recording started")
	}
	if got := launcher.stoppedAt(); !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("check stopped at each launch = %v, want [false true]: the check ends before the recording's child starts\n%s",
			got, logs)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
}

// stopOrderLauncher notes at each launch whether the sound check has already logged its stop by a recording.
type stopOrderLauncher struct {
	*helperLauncher
	logs *logBuffer

	mu      sync.Mutex
	stopped []bool
}

func (l *stopOrderLauncher) Launch() (audiocapture.Process, error) {
	stopped := l.logs.count("sound check stopped: a recording started") > 0
	l.mu.Lock()
	l.stopped = append(l.stopped, stopped)
	l.mu.Unlock()
	return l.helperLauncher.Launch()
}

func (l *stopOrderLauncher) stoppedAt() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.stopped)
}

func TestUnavailableCheckLevels(t *testing.T) {
	_, err := (audiocapture.Unavailable{}).CheckLevels(context.Background(), values.WholeComputer(), time.Second, nil)
	if !errors.Is(err, audiocapture.ErrUnavailable) || !errors.Is(err, recording.ErrCaptureUnavailable) {
		t.Fatalf("CheckLevels = %v", err)
	}
}
