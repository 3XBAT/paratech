package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
			s := NewDiagnosticsService(&fakeSound{r: checked(verdict, values.SoundOK, values.SoundSilence, false)}, discard())
			got, err := s.CheckSound(context.Background())
			if err != nil || got.Verdict != string(verdict) || got.Message != soundMessages[verdict] || got.Hint != "" {
				t.Fatalf("CheckSound = %+v, %v", got, err)
			}
		})
	}
}

func TestCheckSoundJSON(t *testing.T) {
	s := NewDiagnosticsService(&fakeSound{r: checked(values.SoundOK, values.SoundOK, values.SoundSilence, true)}, discard())
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

	got, err := NewDiagnosticsService(&fakeSound{err: secret}, log).CheckSound(context.Background())
	raw, _ := json.Marshal(got)
	if err != nil || got.Verdict != "failed" || got.Message != soundMessages[values.SoundFailed] ||
		strings.Contains(string(raw), "student") || string(raw) == "" {
		t.Fatalf("CheckSound = %s, %v", raw, err)
	}
	if !strings.Contains(logs.String(), "denied") {
		t.Fatalf("the cause must be logged, got %q", logs.String())
	}

	got, err = NewDiagnosticsService(&fakeSound{err: soundcheck.ErrBusy}, log).CheckSound(context.Background())
	if err != nil || got.Verdict != "busy" || got.Message != msgCheckRunning || got.Modes == nil {
		t.Fatalf("a running check = %+v, %v", got, err)
	}

	got, err = NewDiagnosticsService(&fakeSound{r: soundcheck.Result{Verdict: "loud"}}, log).CheckSound(context.Background())
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
	if want := []string{"CheckSound"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("methods %v, want %v", names, want)
	}
}
