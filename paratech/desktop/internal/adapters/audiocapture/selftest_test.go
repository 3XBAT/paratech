package audiocapture_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

const selfTestLine = `{"v":1,"kind":"selftest"}`

// The self-test child (TASK-058, "Check sound") on the fake tone and source: one selftest event per mode, then
// "stopped", exit 0, and no file written.
func TestChildSelfTest(t *testing.T) {
	type want struct{ include, system, hresult string }
	tests := []struct {
		name    string
		src     func(tone *testenv.FakeTone) *testenv.ToneSource
		tone    *testenv.FakeTone
		noTon   bool
		want    want
		streams int // opened streams, all closed again
	}{
		{
			name:    "both hear the tone",
			streams: 2,
			src:     func(tone *testenv.FakeTone) *testenv.ToneSource { return gated(tone) },
			want:    want{"ok", "ok", ""},
		},
		{
			name:    "silence",
			streams: 2,
			src: func(*testenv.FakeTone) *testenv.ToneSource {
				s := gated(nil)
				s.Gate = func() bool { return false }
				return s
			},
			want: want{"silence", "silence", ""},
		},
		{
			name:    "process loopback falls back",
			streams: 2,
			src: func(tone *testenv.FakeTone) *testenv.ToneSource {
				s := gated(tone)
				s.FallbackTo = audiocapture.ModeSystem
				return s
			},
			want: want{"unsupported", "ok", "0x80070005"},
		},
		{
			name:    "no capture in this build",
			streams: 0,
			src: func(tone *testenv.FakeTone) *testenv.ToneSource {
				s := gated(tone)
				s.OpenErr = audiocapture.ErrUnavailable
				return s
			},
			want: want{"unsupported", "unsupported", ""},
		},
		{
			name:    "the device fails",
			streams: 0,
			src: func(tone *testenv.FakeTone) *testenv.ToneSource {
				s := gated(tone)
				s.OpenErr = audiocapture.ErrDevice
				return s
			},
			want: want{"unsupported", "failed", ""},
		},
		{
			name:    "no output device",
			streams: 0,
			src:     func(tone *testenv.FakeTone) *testenv.ToneSource { return gated(tone) },
			tone:    &testenv.FakeTone{NoDevice: true},
			want:    want{"no_device", "no_device", ""},
		},
		{
			name:    "the tone cannot play",
			streams: 2,
			src:     func(tone *testenv.FakeTone) *testenv.ToneSource { return gated(tone) },
			tone:    &testenv.FakeTone{PlayErr: audiocapture.ErrDeviceLost},
			want:    want{"failed", "failed", ""},
		},
		{
			name:    "no tone player",
			streams: 0,
			src:     func(*testenv.FakeTone) *testenv.ToneSource { return gated(nil) },
			noTon:   true,
			want:    want{"failed", "failed", ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			tone := tt.tone
			if tone == nil {
				tone = &testenv.FakeTone{}
			}
			cfg := audiocapture.ChildConfig{Tone: tone, OwnPID: 4242, SelfTest: testenv.FakeSelfTestTiming()}
			if tt.noTon {
				cfg.Tone = nil
			}
			src := tt.src(tone)
			run := startChild(t, context.Background(), src, selfTestLine, cfg)
			if code := run.exit(t); code != audiocapture.ChildExitOK {
				t.Fatalf("exit code %d", code)
			}

			events := run.out.events(t)
			if len(events) != 3 || events[2]["type"] != "stopped" {
				t.Fatalf("events %v, want two selftest events and stopped", events)
			}
			got := map[string]map[string]any{}
			for _, ev := range events[:2] {
				if ev["type"] != "selftest" {
					t.Fatalf("event %v", ev)
				}
				got[ev["mode"].(string)] = ev
			}
			if got["include"]["cause"] != tt.want.include || got["system"]["cause"] != tt.want.system {
				t.Fatalf("include %v, system %v; want %+v", got["include"], got["system"], tt.want)
			}
			if h, _ := got["include"]["hresult"].(string); h != tt.want.hresult {
				t.Fatalf("include hresult %q, want %q", h, tt.want.hresult)
			}
			if tt.want.include == "ok" && got["include"]["device"] != testenv.ToneDevice {
				t.Fatalf("device %v", got["include"]["device"])
			}
			if opened := src.Opened(); len(opened) > 0 && opened[0].Mode == audiocapture.ModeInclude && opened[0].PID != 4242 {
				t.Fatalf("include of %d, want the own process", opened[0].PID)
			}
			if src.Closes() != tt.streams {
				t.Fatalf("%d streams closed, want %d", src.Closes(), tt.streams)
			}
			assertNoFiles(t, run.store)
		})
	}
}

// gated is the source of the self-test: it hears the tone only while tone plays (always with a nil tone).
func gated(tone *testenv.FakeTone) *testenv.ToneSource {
	s := &testenv.ToneSource{Format: s16, PacketFrames: 80, Interval: 2 * time.Millisecond}
	if tone != nil {
		s.Gate = tone.Playing
	}
	return s
}

func assertNoFiles(t *testing.T, store *audiocapture.TestStore) {
	t.Helper()
	root := store.Root()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("the self-test wrote %s", path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// A closed stdin ends a self-test early: the modes are failed, the child still reports and exits cleanly.
func TestChildSelfTestStopsWhenStdinCloses(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	tone := &testenv.FakeTone{}
	timing := testenv.FakeSelfTestTiming()
	timing.Tone = time.Minute
	run := startChild(t, context.Background(), gated(tone), selfTestLine,
		audiocapture.ChildConfig{Tone: tone, SelfTest: timing})
	testenv.Eventually(t, "the tone plays", tone.Playing, nil)
	_ = run.stdin.Close()
	if code := run.exit(t); code != audiocapture.ChildExitOK {
		t.Fatalf("exit code %d", code)
	}
	for _, ev := range run.out.find(t, "selftest") {
		if ev["cause"] == "ok" {
			continue // heard before the stop
		}
		if ev["cause"] != "failed" {
			t.Fatalf("event %v after an early stop", ev)
		}
	}
	if len(run.out.find(t, "stopped")) != 1 {
		t.Fatal("no stopped event")
	}
}

func checkEnv(t *testing.T, timeout time.Duration, scenarios ...string) *recorderEnv {
	t.Helper()
	cfg := testConfig()
	cfg.SelfTestTimeout = timeout
	return newRecorderEnv(t, cfg, scenarios...)
}

// CheckSound runs the self-test in a recorder child launched like the production one (the test binary as the fake
// child) and maps every answer of it.
func TestRecorderCheckSound(t *testing.T) {
	tests := []struct {
		scenario string
		include  values.SoundCause
		system   values.SoundCause
		hands    bool
		hresult  string
	}{
		{scenario: testenv.RecorderTone, include: values.SoundOK, system: values.SoundOK},
		{scenario: testenv.RecorderSilent, include: values.SoundSilence, system: values.SoundSilence},
		{scenario: testenv.RecorderSilent + "," + testenv.RecorderHandsFree, include: values.SoundSilence,
			system: values.SoundSilence, hands: true},
		{scenario: testenv.RecorderFallback, include: values.SoundUnsupported, system: values.SoundOK, hresult: "0x80070005"},
		{scenario: testenv.RecorderUnavailable, include: values.SoundUnsupported, system: values.SoundUnsupported},
		{scenario: testenv.RecorderNoDevice, include: values.SoundNoDevice, system: values.SoundNoDevice},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			e := checkEnv(t, 60*time.Second, tt.scenario)
			check, err := e.rec.CheckSound(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			inc, sys := check.Result(values.CaptureInclude), check.Result(values.CaptureSystem)
			if check.Busy || inc.Cause != tt.include || sys.Cause != tt.system || check.HandsFree != tt.hands ||
				inc.HResult != tt.hresult || len(check.Modes) != 2 {
				t.Fatalf("CheckSound = %+v", check)
			}
			if tt.include != values.SoundNoDevice && check.Device != testenv.ToneDevice {
				t.Fatalf("device %q", check.Device)
			}
			if names := e.store.Names(t, e.chunks); len(names) != 0 {
				t.Fatalf("the sound check wrote %v", names)
			}
		})
	}
}

// A child that never answers is killed at the timeout; a second check meanwhile is refused.
func TestRecorderCheckSoundTimesOut(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := checkEnv(t, 3*time.Second, testenv.RecorderHang)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := e.rec.CheckSound(context.Background())
		done <- err
	}()
	testenv.Eventually(t, "the child is launched", func() bool { return e.launcher.count() == 1 }, nil)
	if _, err := e.rec.CheckSound(context.Background()); !errors.Is(err, audiocapture.ErrSelfTestRunning) {
		t.Fatalf("a second check = %v, want ErrSelfTestRunning", err)
	}
	select {
	case err := <-done:
		if err == nil || time.Since(start) > 30*time.Second {
			t.Fatalf("CheckSound = %v after %v, want the timeout", err, time.Since(start))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("CheckSound did not return after the timeout")
	}
	if e.launcher.count() != 1 {
		t.Fatalf("%d launches", e.launcher.count())
	}
}

// While a recording captures, the check is refused without a child: its tone would disturb the lecture.
func TestRecorderCheckSoundRefusedWhileCapturing(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e := checkEnv(t, 60*time.Second, testenv.RecorderTone)
	ctx := context.Background()
	if err := e.rec.Start(ctx, "rec1", e.chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	check, err := e.rec.CheckSound(ctx)
	if err != nil || !check.Busy || len(check.Modes) != 0 || e.launcher.count() != 1 {
		t.Fatalf("CheckSound while capturing = %+v, %v; %d launches", check, err, e.launcher.count())
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if check, err := e.rec.CheckSound(ctx); err != nil || check.Busy {
		t.Fatalf("CheckSound after the stop = %+v, %v", check, err)
	}
}

// A recording that starts during a check stops it before its own child starts, so the tone stays out of the
// recording; the check answers busy.
func TestRecorderStartStopsTheSoundCheck(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	dataDir := t.TempDir()
	logs := &logBuffer{}
	// The check's child never answers.
	inner := &helperLauncher{dataDir: dataDir, scenarios: []string{testenv.RecorderHang, testenv.RecorderTone}}
	launcher := &stopOrderLauncher{helperLauncher: inner, logs: logs}
	cfg := testConfig()
	cfg.SelfTestTimeout = 60 * time.Second
	store := audiocapture.NewTestStore(filepath.Join(dataDir, "artifacts"))
	rec, err := audiocapture.New(audiocapture.Deps{Files: store, Launcher: launcher, Processes: alive{77: true},
		Log: slog.New(slog.NewTextHandler(logs, nil))}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := &recorderEnv{rec: rec, launcher: inner, store: store, chunks: ref(t, "rec1"), track: ref(t, "rec1.wav")}

	type answer struct {
		check values.SoundCheck
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		check, err := e.rec.CheckSound(context.Background())
		done <- answer{check, err}
	}()
	testenv.Eventually(t, "the check's child is launched", func() bool { return e.launcher.count() == 1 }, nil)

	if err := e.rec.Start(context.Background(), "rec1", e.chunks, values.WholeComputer()); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-done:
		if a.err != nil || !a.check.Busy || len(a.check.Modes) != 0 {
			t.Fatalf("CheckSound stopped by a recording = %+v, %v; want busy", a.check, a.err)
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

func TestUnavailableCheckSound(t *testing.T) {
	if _, err := (audiocapture.Unavailable{}).CheckSound(context.Background()); !errors.Is(err, audiocapture.ErrUnavailable) {
		t.Fatalf("CheckSound = %v", err)
	}
}
