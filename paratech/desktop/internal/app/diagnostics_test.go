package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
	"paratech/desktop/testenv"
)

// The --recorder run mode answers a level check request with the levels of the env's capture source (a fake here:
// tests never touch an audio device), plays nothing and writes nothing.
func TestRecorderModeAnswersTheSoundCheck(t *testing.T) {
	dataDir := t.TempDir()
	src := &testenv.ToneSource{Format: testenv.FakeRecorderFormat(), PacketFrames: 80, Interval: 2 * time.Millisecond}
	a, stdin, out := recorderApp(t, dataDir, src)

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background()) }()
	go func() {
		_, _ = stdin.Write([]byte(`{"v":1,"kind":"levelcheck","target":{"mode":"system","pid":0},"duration_ms":300}` + "\n"))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the level check did not end")
	}
	if !strings.Contains(out.String(), `"type":"level","level":0.5`) || !strings.Contains(out.String(), `"type":"stopped"`) ||
		src.Closes() != 1 {
		t.Fatalf("events %s; %d streams closed", out.String(), src.Closes())
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, artifactsDir, "recordings"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("recordings %v, %v", entries, err)
	}
	// The child never opens the log file: only the instance that holds the lock writes it.
	if _, err := os.Stat(filepath.Join(dataDir, logsDir)); !os.IsNotExist(err) {
		t.Fatalf("the recorder mode created the logs directory: %v", err)
	}
}

// waitSound polls the page until the check is done.
func waitSound(t *testing.T, page *wails.DiagnosticsService) wails.SoundCheck {
	t.Helper()
	var got wails.SoundCheck
	testenv.Eventually(t, "the sound check ends", func() bool {
		var err error
		got, err = page.SoundCheckStatus(context.Background())
		return err == nil && got.State == "done"
	}, nil)
	return got
}

// "Проверить звук" through the core: the levels of the recorder for the remembered source, busy while it captures.
func TestSoundCheckThroughTheCore(t *testing.T) {
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	page := wails.NewDiagnosticsService(wails.DiagnosticsDeps{Sound: core.Sound, Bundle: core.Bundle}, a.log)

	if got, err := page.StartSoundCheck(ctx); err != nil || got.Source != "Весь звук компьютера" {
		t.Fatalf("StartSoundCheck = %+v, %v", got, err)
	}
	if got := waitSound(t, page); got.Verdict != "ok" || got.Peak != 0.5 || !got.Heard {
		t.Fatalf("the check = %+v", got)
	}

	f.recorder.SetLevels([]values.SoundLevel{0, 0.002}, values.SoundOK, nil)
	if _, err := page.StartSoundCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if got := waitSound(t, page); got.Verdict != "silence" || len(got.Hints) == 0 {
		t.Fatalf("a silent check = %+v", got)
	}

	chunks, err := shared.NewArtifactRef(shared.ArtifactRecordings, "rec1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.recorder.Start(ctx, "rec1", chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if _, err := page.StartSoundCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if got := waitSound(t, page); got.Verdict != "busy" {
		t.Fatalf("the check while capturing = %+v", got)
	}
}

// Closing the app ends a running check: its recorder call is cancelled and the session goroutine ends.
func TestCloseStopsTheSoundCheck(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	page := wails.NewDiagnosticsService(wails.DiagnosticsDeps{Sound: core.Sound, Bundle: core.Bundle}, a.log)
	entered, release := f.recorder.HoldLevels()
	defer release()

	if got, err := page.StartSoundCheck(ctx); err != nil || got.State != "running" {
		t.Fatalf("StartSoundCheck = %+v, %v", got, err)
	}
	<-entered
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got, _ := page.SoundCheckStatus(ctx); got.State != "done" {
		t.Fatalf("after Close %+v", got)
	}
}

func TestLevelCheckerOfTheRecorder(t *testing.T) {
	rec := &testenv.FakeRecorder{}
	if levelChecker(rec) != rec {
		t.Fatal("a recorder with a sound check is its own checker")
	}
	if _, ok := levelChecker(audiocapture.Unavailable{}).(audiocapture.Unavailable); !ok {
		t.Fatal("the unavailable recorder answers unsupported")
	}
	if _, ok := levelChecker(noCheckRecorder{}).(audiocapture.Unavailable); !ok {
		t.Fatal("a recorder without a sound check answers unsupported")
	}
}

// noCheckRecorder is a recorder without a sound check.
type noCheckRecorder struct{ audiocapture.Unavailable }

func (noCheckRecorder) CheckLevels() {} // another signature: not a soundcheck.LevelChecker
