package wails

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestSettingsJSONNames(t *testing.T) {
	raw, err := json.Marshal(Settings{RecordingPromptsEnabled: true, ProcessOnlyOnACPower: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"recordingPromptsEnabled":true,"processOnlyOnAcPower":true}`
	if string(raw) != want {
		t.Fatalf("JSON = %s, want %s (frontend/src/bindings/settings.ts reads these names)", raw, want)
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
	if err != nil || b.settings == nil || b.activity == nil || b.prompt == nil || b.schedule == nil || b.models == nil ||
		b.connections == nil || b.diagnostics == nil {
		t.Fatalf("openBindings = %+v, %v", b, err)
	}

	boom := errors.New("no disk")
	opts := validOptions()
	opts.OpenCore = func(context.Context) (Core, error) { return Core{}, boom }
	s, _ = New(opts)
	if _, err := s.openBindings(context.Background()); !errors.Is(err, boom) {
		t.Errorf("failing opener: err = %v", err)
	}

	incomplete := []struct {
		name string
		edit func(*Core)
	}{
		{name: "settings use case", edit: func(c *Core) { c.Settings = nil }},
		{name: "activity use case", edit: func(c *Core) { c.Activity = nil }},
		{name: "recording stopper", edit: func(c *Core) { c.Stopper = nil }},
		{name: "job forcer", edit: func(c *Core) { c.Forcer = nil }},
		{name: "recording prompt use case", edit: func(c *Core) { c.Prompt = nil }},
		{name: "schedule import use case", edit: func(c *Core) { c.Schedule = nil }},
		{name: "schedule file reader", edit: func(c *Core) { c.Files = nil }},
		{name: "model catalog", edit: func(c *Core) { c.Models = nil }},
		{name: "model downloads", edit: func(c *Core) { c.Downloads = nil }},
		{name: "model imports", edit: func(c *Core) { c.Imports = nil }},
		{name: "link opener", edit: func(c *Core) { c.Links = nil }},
		{name: "connections use case", edit: func(c *Core) { c.Connections = nil }},
		{name: "google sign-in use case", edit: func(c *Core) { c.Google = nil }},
		{name: "sound check use case", edit: func(c *Core) { c.Sound = nil }},
		{name: "support bundle use case", edit: func(c *Core) { c.Bundle = nil }},
	}
	for _, tt := range incomplete {
		t.Run("missing "+tt.name, func(t *testing.T) {
			opts := validOptions()
			opts.OpenCore = func(context.Context) (Core, error) {
				c := validCore()
				tt.edit(&c)
				return c, nil
			}
			s, _ := New(opts)
			if _, err := s.openBindings(context.Background()); err == nil {
				t.Errorf("a core without the %s must fail", tt.name)
			}
		})
	}
}

// TestFrontendCallsBoundNames keeps the method names in the frontend wrappers equal to what Wails binds: the package
// path, the type name and the method name (bindings.go in Wails v3). The page calls them by name, so a rename on
// either side would only fail at run time.
func TestFrontendCallsBoundNames(t *testing.T) {
	tests := []struct {
		file    string
		typ     reflect.Type
		methods int
	}{
		{file: "settings.ts", typ: reflect.TypeFor[*SettingsService](), methods: 3},
		{file: "activity.ts", typ: reflect.TypeFor[*ActivityService](), methods: 4},
		{file: "prompt.ts", typ: reflect.TypeFor[*PromptService](), methods: 5},
		{file: "schedule.ts", typ: reflect.TypeFor[*ScheduleImportService](), methods: 3},
		{file: "models.ts", typ: reflect.TypeFor[*ModelsService](), methods: 13},
		{file: "connections.ts", typ: reflect.TypeFor[*ConnectionsService](), methods: 9},
		{file: "google.ts", typ: reflect.TypeFor[*GoogleService](), methods: 6},
		{file: "diagnostics.ts", typ: reflect.TypeFor[*DiagnosticsService](), methods: 2},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src, err := os.ReadFile("../../../frontend/src/bindings/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if tt.typ.NumMethod() != tt.methods {
				t.Errorf("%s has %d exported methods, want %d: every exported method is callable from the page",
					tt.typ.Elem().Name(), tt.typ.NumMethod(), tt.methods)
			}
			for i := range tt.typ.NumMethod() {
				name := tt.typ.Elem().PkgPath() + "." + tt.typ.Elem().Name() + "." + tt.typ.Method(i).Name
				if !strings.Contains(string(src), `"`+name+`"`) {
					t.Errorf("frontend/src/bindings/%s does not call %q", tt.file, name)
				}
			}
		})
	}
}
