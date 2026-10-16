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

// The --recorder run mode answers a self-test request with the tone player of the env (a fake here: tests never play
// sound) and writes nothing.
func TestRecorderModeAnswersTheSoundCheck(t *testing.T) {
	dataDir := t.TempDir()
	tone := &testenv.FakeTone{}
	src := &testenv.ToneSource{Format: testenv.FakeRecorderFormat(), PacketFrames: 80, Interval: 2 * time.Millisecond,
		Gate: tone.Playing}
	a, stdin, out := recorderApp(t, dataDir, src)
	a.env.tone = tone

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background()) }()
	go func() { _, _ = stdin.Write([]byte(`{"v":1,"kind":"selftest"}` + "\n")) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the self-test did not end")
	}
	if n := strings.Count(out.String(), `"type":"selftest","mode":`); n != 2 || tone.Plays() != 1 {
		t.Fatalf("events %s; %d plays", out.String(), tone.Plays())
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

// "Проверить звук" through the core: the answer of the recorder for the remembered source, busy while it captures.
func TestSoundCheckThroughTheCore(t *testing.T) {
	ctx := context.Background()
	a, f := newTestAppWith(t, t.TempDir())
	core := startCore(t, a)
	page := wails.NewDiagnosticsService(wails.DiagnosticsDeps{Sound: core.Sound, Bundle: core.Bundle}, a.log)

	got, err := page.CheckSound(ctx)
	if err != nil || got.Verdict != "ok" || got.Device != testenv.ToneDevice || len(got.Modes) != 2 {
		t.Fatalf("CheckSound = %+v, %v", got, err)
	}

	// The whole computer is the default source: classic loopback is its final fallback.
	f.recorder.SetSoundCheck(values.SoundCheck{Modes: []values.ModeCheck{
		{Mode: values.CaptureInclude, Cause: values.SoundUnsupported}, {Mode: values.CaptureSystem, Cause: values.SoundSilence},
	}}, nil)
	if got, err := page.CheckSound(ctx); err != nil || got.Verdict != "silence" {
		t.Fatalf("CheckSound = %+v, %v", got, err)
	}

	chunks, err := shared.NewArtifactRef(shared.ArtifactRecordings, "rec1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.recorder.Start(ctx, "rec1", chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	if got, err := page.CheckSound(ctx); err != nil || got.Verdict != "busy" {
		t.Fatalf("CheckSound while capturing = %+v, %v", got, err)
	}
}

func TestSoundCheckerOfTheRecorder(t *testing.T) {
	rec := &testenv.FakeRecorder{}
	if soundChecker(rec) != rec {
		t.Fatal("a recorder with a sound check is its own checker")
	}
	if _, ok := soundChecker(audiocapture.Unavailable{}).(audiocapture.Unavailable); !ok {
		t.Fatal("the unavailable recorder answers unsupported")
	}
	if _, ok := soundChecker(noCheckRecorder{}).(audiocapture.Unavailable); !ok {
		t.Fatal("a recorder without a sound check answers unsupported")
	}
}

// noCheckRecorder is a recorder without a sound check.
type noCheckRecorder struct{ audiocapture.Unavailable }

func (noCheckRecorder) CheckSound() {} // another signature: not a soundcheck.SoundChecker
