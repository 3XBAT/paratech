package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/domain/recording/values"
)

// fakeSound is the sound check behind the binding.
type fakeSound struct {
	st       soundcheck.Status
	err      error
	stopped  bool
	deadline bool // the ctx of Stop had a deadline
}

func (f *fakeSound) Start(context.Context) (soundcheck.Status, error) { return f.st, f.err }
func (f *fakeSound) Status() soundcheck.Status                        { return f.st }

func (f *fakeSound) Stop(ctx context.Context) soundcheck.Status {
	f.stopped = true
	_, f.deadline = ctx.Deadline()
	return f.st
}

func soundPage(f *fakeSound, log *slog.Logger) *DiagnosticsService {
	return NewDiagnosticsService(DiagnosticsDeps{Sound: f, Bundle: &fakeBundle{}, Picker: &fakePicker{}}, log)
}

func zoom(t *testing.T) values.Source {
	t.Helper()
	s, err := values.SourceOf("zoom.exe")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func done(source values.Source, verdict values.SoundCause, peak values.SoundLevel) soundcheck.Status {
	return soundcheck.Status{State: soundcheck.StateDone, Source: source, Check: values.LevelCheck{Peak: peak},
		Heard: peak.Heard(), Elapsed: 15 * time.Second, Duration: 15 * time.Second, Verdict: verdict}
}

func TestSoundCheckAnswersEveryVerdict(t *testing.T) {
	for verdict, text := range soundTexts {
		t.Run(string(verdict), func(t *testing.T) {
			for _, source := range []values.Source{values.WholeComputer(), zoom(t)} {
				got, err := soundPage(&fakeSound{st: done(source, verdict, 0)}, discard()).SoundCheckStatus(context.Background())
				if err != nil || got.State != "done" || got.Verdict != string(verdict) || got.Message != text.message ||
					got.Hints == nil {
					t.Fatalf("SoundCheckStatus = %+v, %v", got, err)
				}
				if verdict != values.SoundOK && verdict != values.SoundBusy && len(got.Hints) == 0 {
					t.Fatalf("%s has no hint", verdict)
				}
			}
		})
	}

	// Silence of an app points at the app; of the whole computer at the mixer.
	app, err := soundPage(&fakeSound{st: done(zoom(t), values.SoundSilence, 0.001)}, discard()).SoundCheckStatus(context.Background())
	if err != nil || !slices.Equal(app.Hints, silenceHintsApp) || app.Source != "zoom.exe" || app.WholeComputer {
		t.Fatalf("silence of an app = %+v", app)
	}
	whole, err := soundPage(&fakeSound{st: done(values.WholeComputer(), values.SoundSilence, 0)}, discard()).
		SoundCheckStatus(context.Background())
	if err != nil || !slices.Equal(whole.Hints, soundTexts[values.SoundSilence].hints) || whole.Source != sourceWholeComputer ||
		!whole.WholeComputer {
		t.Fatalf("silence of the whole computer = %+v", whole)
	}

	// An unknown verdict reads as failed.
	got, err := soundPage(&fakeSound{st: done(values.WholeComputer(), "loud", 0)}, discard()).SoundCheckStatus(context.Background())
	if err != nil || got.Verdict != "failed" || got.Message != soundTexts[values.SoundFailed].message {
		t.Fatalf("an unknown verdict = %+v, %v", got, err)
	}
}

func TestSoundCheckJSON(t *testing.T) {
	running := soundcheck.Status{State: soundcheck.StateRunning, Source: zoom(t),
		Check: values.LevelCheck{Level: 0.12345, Peak: 0.5}, Heard: true, Elapsed: 4200 * time.Millisecond,
		Duration: 15 * time.Second}
	got, err := soundPage(&fakeSound{st: running}, discard()).StartSoundCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"state":"running","level":0.123,"peak":0.5,"heard":true,"elapsedMs":4200,"remainingMs":10800,` +
		`"durationMs":15000,"verdict":"","message":"` + msgHeard + `","hints":[],"source":"zoom.exe","wholeComputer":false}`
	if string(raw) != want {
		t.Fatalf("JSON = %s\nwant %s (frontend/src/bindings/diagnostics.ts reads these names)", raw, want)
	}

	quiet := running
	quiet.Source, quiet.Check, quiet.Heard = values.WholeComputer(), values.LevelCheck{}, false
	got, _ = soundPage(&fakeSound{st: quiet}, discard()).SoundCheckStatus(context.Background())
	if got.Message != msgPlaySomething || got.Source != sourceWholeComputer || !got.WholeComputer || got.Verdict != "" {
		t.Fatalf("a quiet running check = %+v", got)
	}

	idle, _ := soundPage(&fakeSound{st: soundcheck.Status{State: soundcheck.StateIdle, Duration: 15 * time.Second}}, discard()).
		SoundCheckStatus(context.Background())
	raw, _ = json.Marshal(idle)
	if string(raw) != `{"state":"idle","level":0,"peak":0,"heard":false,"elapsedMs":0,"remainingMs":15000,`+
		`"durationMs":15000,"verdict":"","message":"","hints":[],"source":"","wholeComputer":false}` {
		t.Fatalf("idle JSON = %s", raw)
	}
}

func TestStartSoundCheckHidesErrors(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	secret := errors.New(`open C:\Users\student\AppData: denied`)

	got, err := soundPage(&fakeSound{err: secret}, log).StartSoundCheck(context.Background())
	raw, _ := json.Marshal(got)
	if err != nil || got.State != "done" || got.Verdict != "failed" || strings.Contains(string(raw), "student") {
		t.Fatalf("StartSoundCheck = %s, %v", raw, err)
	}
	if !strings.Contains(logs.String(), "denied") {
		t.Fatalf("the cause must be logged, got %q", logs.String())
	}

	// A check that runs already is the answer.
	running := soundcheck.Status{State: soundcheck.StateRunning, Duration: 15 * time.Second}
	got, err = soundPage(&fakeSound{st: running, err: soundcheck.ErrBusy}, log).StartSoundCheck(context.Background())
	if err != nil || got.State != "running" || got.Message != msgPlaySomething {
		t.Fatalf("a running check = %+v, %v", got, err)
	}
}

func TestStopSoundCheck(t *testing.T) {
	f := &fakeSound{st: done(values.WholeComputer(), values.SoundOK, 0.4)}
	got, err := soundPage(f, discard()).StopSoundCheck(context.Background())
	if err != nil || !f.stopped || !f.deadline || got.Verdict != "ok" || got.Message != msgHeard || got.Peak != 0.4 {
		t.Fatalf("StopSoundCheck = %+v, %v; stopped %v, bounded %v", got, err, f.stopped, f.deadline)
	}
}

// The page may call exactly these methods (TestFrontendCallsBoundNames checks the frontend file).
func TestDiagnosticsServiceMethods(t *testing.T) {
	typ := reflect.TypeFor[*DiagnosticsService]()
	var names []string
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	if want := []string{"SaveSupportBundle", "SoundCheckStatus", "StartSoundCheck", "StopSoundCheck"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("methods %v, want %v", names, want)
	}
}

// fakeBundle is the support bundle behind the binding.
type fakeBundle struct {
	path string
	err  error
}

func (f *fakeBundle) SuggestedName() string { return "paratech-support-20261016-090507.zip" }

func (f *fakeBundle) Create(_ context.Context, path string) error {
	f.path = path
	return f.err
}

// fakePicker is the save dialog: it answers path (and records the proposed name).
type fakePicker struct {
	path, name string
	err        error
}

func (f *fakePicker) PickBundleFile(_ context.Context, name string) (string, error) {
	f.name = name
	return f.path, f.err
}

func TestSaveSupportBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "student-folder", "support.zip")
	tests := []struct {
		name    string
		picker  *fakePicker
		bundle  *fakeBundle
		want    BundleResult
		created bool
	}{
		{"saved", &fakePicker{path: path}, &fakeBundle{},
			BundleResult{Status: "saved", Message: msgBundleSaved, Name: "support.zip"}, true},
		{"cancelled", &fakePicker{}, &fakeBundle{}, BundleResult{Status: "cancelled", Message: msgBundleCancelled}, false},
		{"the dialog fails", &fakePicker{err: errors.New("no window")}, &fakeBundle{},
			BundleResult{Status: "failed", Message: msgBundleFailed}, false},
		{"the bundle fails", &fakePicker{path: path}, &fakeBundle{err: errors.New("zipfile: disk full")},
			BundleResult{Status: "failed", Message: msgBundleFailed}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			s := NewDiagnosticsService(DiagnosticsDeps{Sound: &fakeSound{}, Bundle: tt.bundle, Picker: tt.picker},
				slog.New(slog.NewTextHandler(&logs, nil)))
			got, err := s.SaveSupportBundle(context.Background())
			if err != nil || got != tt.want {
				t.Fatalf("SaveSupportBundle = %+v, %v; want %+v", got, err, tt.want)
			}
			if tt.picker.name != "paratech-support-20261016-090507.zip" || (tt.bundle.path == path) != tt.created {
				t.Fatalf("proposed %q, created at %q", tt.picker.name, tt.bundle.path)
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "student-folder") || strings.Contains(logs.String(), "student-folder") {
				t.Fatalf("the path leaked: %s / %s", raw, logs.String())
			}
		})
	}
	raw, _ := json.Marshal(BundleResult{Status: "saved", Message: "m", Name: "n"})
	if string(raw) != `{"status":"saved","message":"m","name":"n"}` {
		t.Fatalf("JSON = %s (frontend/src/bindings/diagnostics.ts reads these names)", raw)
	}
}
