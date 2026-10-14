package wails

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/preferences"
)

// fakeSettings is the use case behind the binding.
type fakeSettings struct {
	t   preferences.Toggles
	err error
}

func (f *fakeSettings) Get(context.Context) (preferences.Toggles, error) { return f.t, f.err }

func (f *fakeSettings) SetRecordingPromptsEnabled(_ context.Context, on bool) (preferences.Toggles, error) {
	if f.err != nil {
		return preferences.Toggles{}, f.err
	}
	f.t.RecordingPromptsEnabled = on
	return f.t, nil
}

func (f *fakeSettings) SetProcessOnlyOnACPower(_ context.Context, on bool) (preferences.Toggles, error) {
	if f.err != nil {
		return preferences.Toggles{}, f.err
	}
	f.t.ProcessOnlyOnACPower = on
	return f.t, nil
}

func TestSettingsServiceMapsToggles(t *testing.T) {
	ctx := context.Background()
	f := &fakeSettings{t: preferences.Toggles{RecordingPromptsEnabled: true, ProcessOnlyOnACPower: true}}
	s := NewSettingsService(f, discard())

	got, err := s.GetSettings(ctx)
	if err != nil || got != (Settings{RecordingPromptsEnabled: true, ProcessOnlyOnACPower: true}) {
		t.Fatalf("GetSettings = %+v, %v", got, err)
	}
	got, err = s.SetRecordingPromptsEnabled(ctx, false)
	if err != nil || got != (Settings{ProcessOnlyOnACPower: true}) {
		t.Fatalf("SetRecordingPromptsEnabled(false) = %+v, %v", got, err)
	}
	got, err = s.SetProcessOnlyOnACPower(ctx, false)
	if err != nil || got != (Settings{}) {
		t.Fatalf("SetProcessOnlyOnACPower(false) = %+v, %v", got, err)
	}
}

func TestSettingsServiceHidesErrorDetails(t *testing.T) {
	var logs bytes.Buffer
	secret := errors.New(`open C:\Users\student\db: locked`)
	s := NewSettingsService(&fakeSettings{err: secret}, slog.New(slog.NewTextHandler(&logs, nil)))

	calls := map[string]func() (Settings, error){
		"GetSettings":                func() (Settings, error) { return s.GetSettings(context.Background()) },
		"SetRecordingPromptsEnabled": func() (Settings, error) { return s.SetRecordingPromptsEnabled(context.Background(), true) },
		"SetProcessOnlyOnACPower":    func() (Settings, error) { return s.SetProcessOnlyOnACPower(context.Background(), true) },
	}
	for name, call := range calls {
		_, err := call()
		if !errors.Is(err, ErrSettingsUnavailable) || strings.Contains(err.Error(), "student") {
			t.Errorf("%s: err = %v, want only ErrSettingsUnavailable", name, err)
		}
	}
	if !strings.Contains(logs.String(), "locked") {
		t.Errorf("the cause must be logged, got %q", logs.String())
	}
}

func TestOpenBindings(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.openBindings(context.Background())
	if err != nil || b.settings == nil {
		t.Fatalf("openBindings = %+v, %v", b, err)
	}

	boom := errors.New("no disk")
	opts := validOptions()
	opts.OpenCore = func(context.Context) (Core, error) { return Core{}, boom }
	s, _ = New(opts)
	if _, err := s.openBindings(context.Background()); !errors.Is(err, boom) {
		t.Errorf("failing opener: err = %v", err)
	}

	opts.OpenCore = func(context.Context) (Core, error) { return Core{}, nil }
	s, _ = New(opts)
	if _, err := s.openBindings(context.Background()); err == nil {
		t.Error("a core without the settings use case must fail")
	}
}

// TestFrontendCallsBoundNames keeps the method names in the frontend wrapper equal to what Wails binds: the package
// path, the type name and the method name (bindings.go in Wails v3). The page calls them by name, so a rename on
// either side would only fail at run time.
func TestFrontendCallsBoundNames(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/settings.ts")
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeFor[*SettingsService]()
	if typ.NumMethod() != 3 {
		t.Errorf("SettingsService has %d exported methods, want 3: every exported method is callable from the page", typ.NumMethod())
	}
	for i := range typ.NumMethod() {
		name := typ.Elem().PkgPath() + "." + typ.Elem().Name() + "." + typ.Method(i).Name
		if !strings.Contains(string(src), `"`+name+`"`) {
			t.Errorf("frontend/src/bindings/settings.ts does not call %q", name)
		}
	}
}
