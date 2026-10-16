package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/domain/recording/values"
)

// fakeSound is the sound check behind the binding.
type fakeSound struct {
	r   soundcheck.Result
	err error
}

func (f *fakeSound) Check(context.Context) (soundcheck.Result, error) { return f.r, f.err }

func soundPage(f *fakeSound, log *slog.Logger) *DiagnosticsService {
	return NewDiagnosticsService(DiagnosticsDeps{Sound: f, Bundle: &fakeBundle{}, Picker: &fakePicker{}}, log)
}

func checked(verdict, include, system values.SoundCause, handsFree bool) soundcheck.Result {
	return soundcheck.Result{Verdict: verdict, Check: values.SoundCheck{
		Device: "Динамики (Realtek(R) Audio)", HandsFree: handsFree, Modes: []values.ModeCheck{
			{Mode: values.CaptureInclude, Cause: include, HResult: "0x80070005"}, {Mode: values.CaptureSystem, Cause: system},
		},
	}}
}

func TestCheckSoundAnswersEveryVerdict(t *testing.T) {
	for verdict := range soundMessages {
		t.Run(string(verdict), func(t *testing.T) {
			s := soundPage(&fakeSound{r: checked(verdict, values.SoundOK, values.SoundSilence, false)}, discard())
			got, err := s.CheckSound(context.Background())
			if err != nil || got.Verdict != string(verdict) || got.Message != soundMessages[verdict] || got.Hint != "" {
				t.Fatalf("CheckSound = %+v, %v", got, err)
			}
		})
	}
}

func TestCheckSoundJSON(t *testing.T) {
	s := soundPage(&fakeSound{r: checked(values.SoundOK, values.SoundOK, values.SoundSilence, true)}, discard())
	got, err := s.CheckSound(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"verdict":"ok","message":"` + soundMessages[values.SoundOK] + `","device":"Динамики (Realtek(R) Audio)",` +
		`"handsFree":true,"hint":"` + msgHandsFreeHint + `","modes":[{"mode":"include","ok":true,"cause":"ok"},` +
		`{"mode":"system","ok":false,"cause":"silence"}]}`
	if string(raw) != want {
		t.Fatalf("JSON = %s\nwant %s (frontend/src/bindings/diagnostics.ts reads these names)", raw, want)
	}
}

func TestCheckSoundHidesErrors(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	secret := errors.New(`open C:\Users\student\AppData: denied`)

	got, err := soundPage(&fakeSound{err: secret}, log).CheckSound(context.Background())
	raw, _ := json.Marshal(got)
	if err != nil || got.Verdict != "failed" || got.Message != soundMessages[values.SoundFailed] ||
		strings.Contains(string(raw), "student") || string(raw) == "" {
		t.Fatalf("CheckSound = %s, %v", raw, err)
	}
	if !strings.Contains(logs.String(), "denied") {
		t.Fatalf("the cause must be logged, got %q", logs.String())
	}

	got, err = soundPage(&fakeSound{err: soundcheck.ErrBusy}, log).CheckSound(context.Background())
	if err != nil || got.Verdict != "busy" || got.Message != msgCheckRunning || got.Modes == nil {
		t.Fatalf("a running check = %+v, %v", got, err)
	}

	got, err = soundPage(&fakeSound{r: soundcheck.Result{Verdict: "loud"}}, log).CheckSound(context.Background())
	if err != nil || got.Verdict != "failed" {
		t.Fatalf("an unknown verdict = %+v, %v", got, err)
	}
}

// The page may call exactly these methods (TestFrontendCallsBoundNames checks the frontend file once it exists).
func TestDiagnosticsServiceMethods(t *testing.T) {
	typ := reflect.TypeFor[*DiagnosticsService]()
	var names []string
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	if want := []string{"CheckSound", "SaveSupportBundle"}; !reflect.DeepEqual(names, want) {
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
