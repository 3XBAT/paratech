package audiocapture_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// fakeFinder is the running apps of a fake Windows: at most one instance per app.
type fakeFinder struct {
	mu      sync.Mutex
	running map[shared.AppName]audiocapture.ProcessIdentity
	sameErr error
}

func newFakeFinder(procs ...audiocapture.ProcessIdentity) *fakeFinder {
	f := &fakeFinder{running: map[shared.AppName]audiocapture.ProcessIdentity{}}
	for _, p := range procs {
		f.running[p.App] = p
	}
	return f
}

func (f *fakeFinder) Find(_ context.Context, app shared.AppName) (audiocapture.ProcessIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.running[app]
	if !ok {
		return audiocapture.ProcessIdentity{}, fmt.Errorf("%w: %s has no window", audiocapture.ErrAppNotRunning, app)
	}
	return p, nil
}

// Same compares the whole identity, so a PID reused by another app or a new instance is not the same.
func (f *fakeFinder) Same(_ context.Context, id audiocapture.ProcessIdentity) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sameErr != nil {
		return false, f.sameErr
	}
	return f.running[id.App] == id, nil
}

// set replaces the running apps.
func (f *fakeFinder) set(procs ...audiocapture.ProcessIdentity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clear(f.running)
	for _, p := range procs {
		f.running[p.App] = p
	}
}

func (f *fakeFinder) failSame(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sameErr = err
}

// logBuffer collects the log of a Recorder.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logBuffer) count(msg string) int { return strings.Count(l.String(), `msg="`+msg+`"`) }

var (
	zoom1 = audiocapture.ProcessIdentity{App: "zoom.exe", PID: 300, Created: 1}
	zoom2 = audiocapture.ProcessIdentity{App: "zoom.exe", PID: 301, Created: 2}
	zoom3 = audiocapture.ProcessIdentity{App: "zoom.exe", PID: 302, Created: 3}
)

func rebindConfig() audiocapture.Config {
	cfg := testConfig()
	cfg.RebindInterval = 20 * time.Millisecond
	return cfg
}

func newAppEnv(t *testing.T, cfg audiocapture.Config, finder *fakeFinder, scenarios ...string) (*recorderEnv, *logBuffer) {
	t.Helper()
	dataDir := t.TempDir()
	store := audiocapture.NewTestStore(filepath.Join(dataDir, "artifacts"))
	launcher := &helperLauncher{dataDir: dataDir, scenarios: scenarios}
	logs := &logBuffer{}
	deps := audiocapture.Deps{Files: store, Launcher: launcher, Log: slog.New(slog.NewTextHandler(logs, nil))}
	if finder != nil {
		deps.Apps = finder
	}
	rec, err := audiocapture.New(deps, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &recorderEnv{rec: rec, launcher: launcher, store: store, chunks: ref(t, "rec1"), track: ref(t, "rec1.wav")}, logs
}

func zoomSource(t *testing.T) values.Source {
	t.Helper()
	s, err := values.SourceOf("zoom.exe")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The app restarts during the recording: the capture follows its new instance by the executable name, without a
// restart of the budget, and the chunks go on.
func TestRecorderRebindsARestartedApp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	finder := newFakeFinder(zoom1)
	cfg := rebindConfig()
	cfg.MaxRestarts = 0 // a re-bind is no restart
	e, logs := newAppEnv(t, cfg, finder, testenv.RecorderTone)

	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	if d := e.diag(t); d.TargetMode != audiocapture.ModeInclude || d.TargetPID != 300 || d.App != "zoom.exe" {
		t.Fatalf("diagnostics = %+v, want INCLUDE of zoom's process", d)
	}
	e.waitDiag(t, "the first chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })

	finder.set() // zoom exits
	e.waitDiag(t, "the capture waits for zoom", func(d audiocapture.Diagnostics) bool { return d.WaitingForApp })
	finder.set(zoom2)
	e.waitDiag(t, "the new instance is captured", func(d audiocapture.Diagnostics) bool {
		return d.Rebinds == 1 && d.Running && !d.WaitingForApp && d.TargetPID == 301
	})
	chunks := e.diag(t).Chunks
	e.waitDiag(t, "the new child closes a chunk", func(d audiocapture.Diagnostics) bool { return d.Chunks > chunks })
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}

	names := e.names(t)
	for i, n := range names {
		if want := fmt.Sprintf("chunk-%06d.wav", i+1); n != want {
			t.Fatalf("chunks = %v, want closed chunks numbered on", names)
		}
	}
	if d := e.diag(t); d.Restarts != 0 || e.launcher.count() != 2 {
		t.Fatalf("restarts %d, %d children; want one re-bind child and no restart", d.Restarts, e.launcher.count())
	}
	if _, err := e.rec.Finalize(context.Background(), e.chunks, e.track); err != nil {
		t.Fatal(err)
	}
	if logs.count("capture target exited; waiting for the app") != 1 || logs.count("capture target re-bound by exe name") != 1 {
		t.Fatalf("log:\n%s", logs)
	}
	if !strings.Contains(logs.String(), "old_pid=300 new_pid=301") {
		t.Fatalf("the re-bind does not log both PIDs:\n%s", logs)
	}
}

// The app is gone and never comes back: the recording runs on and stops cleanly.
func TestRecorderWaitsForAnAppThatDoesNotComeBack(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	tests := []struct {
		name string
		now  []audiocapture.ProcessIdentity
	}{
		{"exited", nil},
		// The PID now runs another program: not zoom any more.
		{"pid reused by another program", []audiocapture.ProcessIdentity{{App: "notepad.exe", PID: 300, Created: 1}}},
		{"pid reused by a new zoom", []audiocapture.ProcessIdentity{{App: "zoom.exe", PID: 300, Created: 9}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finder := newFakeFinder(zoom1)
			cfg := rebindConfig()
			cfg.MaxRebinds = 0 // the new zoom of the last case is not followed
			e, logs := newAppEnv(t, cfg, finder, testenv.RecorderTone)
			if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
				t.Fatal(err)
			}
			finder.set(tt.now...)
			e.waitDiag(t, "the capture waits for zoom", func(d audiocapture.Diagnostics) bool { return d.WaitingForApp })
			time.Sleep(5 * cfg.RebindInterval)
			if err := stop(t, e); err != nil {
				t.Fatalf("Stop = %v", err)
			}
			if err := e.waitEnded(t); err != nil {
				t.Fatalf("Wait = %v", err)
			}
			if d := e.diag(t); d.Rebinds != 0 || d.Restarts != 0 || e.launcher.count() != 1 {
				t.Fatalf("diagnostics %+v, %d children", d, e.launcher.count())
			}
			if logs.count("capture target exited; waiting for the app") != 1 {
				t.Fatalf("log:\n%s", logs)
			}
		})
	}
}

// A child that dies while the app is gone uses no restart: the capture waits for the app without a child, then
// follows its new instance; a stop during the wait is clean.
func TestRecorderWaitsWithoutAChild(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Run("the app comes back", func(t *testing.T) {
		finder := newFakeFinder(zoom1)
		cfg := rebindConfig()
		cfg.MaxRestarts = 0
		e, _ := newAppEnv(t, cfg, finder, testenv.RecorderCrash, testenv.RecorderTone)
		if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
			t.Fatal(err)
		}
		finder.set()
		e.waitDiag(t, "the crashed child is not restarted", func(d audiocapture.Diagnostics) bool {
			return d.WaitingForApp && !d.Running
		})
		time.Sleep(5 * cfg.RebindInterval)
		if n := e.launcher.count(); n != 1 {
			t.Fatalf("%d children while the app is gone, want 1", n)
		}
		finder.set(zoom2)
		e.waitDiag(t, "the new instance is captured", func(d audiocapture.Diagnostics) bool { return d.Rebinds == 1 && d.Running })
		if err := stop(t, e); err != nil {
			t.Fatal(err)
		}
		if err := e.waitEnded(t); err != nil {
			t.Fatalf("Wait = %v", err)
		}
		if d := e.diag(t); d.Restarts != 0 || d.TargetPID != 301 {
			t.Fatalf("diagnostics = %+v", d)
		}
	})
	t.Run("stop during the wait", func(t *testing.T) {
		finder := newFakeFinder(zoom1)
		e, _ := newAppEnv(t, rebindConfig(), finder, testenv.RecorderCrash)
		if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
			t.Fatal(err)
		}
		finder.set()
		e.waitDiag(t, "the capture waits without a child", func(d audiocapture.Diagnostics) bool {
			return d.WaitingForApp && !d.Running
		})
		if err := stop(t, e); err != nil {
			t.Fatalf("Stop = %v", err)
		}
		if err := e.waitEnded(t); err != nil {
			t.Fatalf("Wait = %v", err)
		}
	})
}

// Past MaxRebinds the capture keeps waiting for the app and logs that once.
func TestRecorderCapsTheRebinds(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	finder := newFakeFinder(zoom1)
	cfg := rebindConfig()
	cfg.MaxRebinds = 1
	e, logs := newAppEnv(t, cfg, finder, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	finder.set(zoom2)
	e.waitDiag(t, "the first re-bind", func(d audiocapture.Diagnostics) bool { return d.Rebinds == 1 && d.Running })
	finder.set(zoom3)
	e.waitDiag(t, "the capture waits again", func(d audiocapture.Diagnostics) bool { return d.WaitingForApp })
	time.Sleep(5 * cfg.RebindInterval)
	if d := e.diag(t); d.Rebinds != 1 || d.TargetPID != 301 || !d.WaitingForApp {
		t.Fatalf("diagnostics = %+v, want no second re-bind", d)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if logs.count("the app restarted too often; the capture waits without following it") != 1 {
		t.Fatalf("log:\n%s", logs)
	}
}

// An app whose process cannot be checked is taken as running; a capture that fell back to system mode records
// everything and does not follow the app.
func TestRecorderKeepsTheTargetWhenUnsure(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	tests := []struct {
		name     string
		scenario string
		change   func(f *fakeFinder)
		mode     audiocapture.Mode
	}{
		{"check fails", testenv.RecorderTone, func(f *fakeFinder) { f.failSame(errors.New("access denied")); f.set(zoom2) }, audiocapture.ModeInclude},
		{"fallen back to system mode", testenv.RecorderFallback, func(f *fakeFinder) { f.set(zoom2) }, audiocapture.ModeSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finder := newFakeFinder(zoom1)
			cfg := rebindConfig()
			e, _ := newAppEnv(t, cfg, finder, tt.scenario)
			if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
				t.Fatal(err)
			}
			tt.change(finder)
			time.Sleep(10 * cfg.RebindInterval)
			if d := e.diag(t); d.WaitingForApp || d.Rebinds != 0 || d.Mode != tt.mode || !d.Running {
				t.Fatalf("diagnostics = %+v", d)
			}
			if err := stop(t, e); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// restartedFinder is an app that restarted right after the recording found it: the first Find returns zoom1, every
// later one zoom2. Only the recorder calls it, so the test goroutine shares no lock with the supervisor.
type restartedFinder struct {
	mu    sync.Mutex
	finds int
}

func (f *restartedFinder) Find(context.Context, shared.AppName) (audiocapture.ProcessIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finds++
	if f.finds == 1 {
		return zoom1, nil
	}
	return zoom2, nil
}

func (f *restartedFinder) Same(_ context.Context, id audiocapture.ProcessIdentity) (bool, error) {
	return id == zoom2, nil
}

// A re-bind right after Start returned does not race with Start's log of the app. No logger and no diagnostics
// read until the re-bind is done: a shared lock would order the accesses and hide the race from -race.
func TestRecorderRebindRightAfterStart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dataDir := t.TempDir()
	store := audiocapture.NewTestStore(filepath.Join(dataDir, "artifacts"))
	launcher := &helperLauncher{dataDir: dataDir, scenarios: []string{testenv.RecorderTone}}
	cfg := rebindConfig()
	cfg.RebindInterval = time.Millisecond
	rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: launcher, Apps: &restartedFinder{}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := &recorderEnv{rec: rec, launcher: launcher, store: store, chunks: ref(t, "rec1"), track: ref(t, "rec1.wav")}

	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second) // the re-bind happens meanwhile; polling would take a lock
	// Running: the new child has started. The re-bind shows before its launch, and a stop then would leave a cold
	// start of the child on a busy runner only StopGrace to start and stop.
	e.waitDiag(t, "zoom2 is captured", func(d audiocapture.Diagnostics) bool {
		return d.Rebinds == 1 && d.TargetPID == 301 && d.Running
	})
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}
}

func TestRecorderRefusesAnAppThatDoesNotRun(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e, _ := newAppEnv(t, rebindConfig(), newFakeFinder(), testenv.RecorderTone)
	err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t))
	if !errors.Is(err, recording.ErrSourceNotRunning) || !errors.Is(err, audiocapture.ErrAppNotRunning) {
		t.Fatalf("Start = %v, want ErrSourceNotRunning", err)
	}
	if _, ok := e.rec.Diagnostics(context.Background(), "rec1"); ok || e.launcher.count() != 0 {
		t.Fatal("a start of an app that does not run left a capture or launched a child")
	}

	noFinder, _ := newAppEnv(t, rebindConfig(), nil, testenv.RecorderTone)
	if err := noFinder.rec.Start(context.Background(), "rec1", noFinder.chunks, zoomSource(t)); !errors.Is(err, audiocapture.ErrInvalidTarget) {
		t.Fatalf("Start without an app finder = %v, want ErrInvalidTarget", err)
	}
	if err := e.rec.Start(context.Background(), "../x", e.chunks, zoomSource(t)); err == nil || errors.Is(err, recording.ErrSourceNotRunning) {
		t.Fatalf("Start with a bad id = %v, want the id refused first", err)
	}
	if !slices.Equal(e.names(t), []string{}) {
		t.Fatal("chunks were written")
	}
}
