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
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// fakeSettings is the use case behind the binding.
type fakeSettings struct {
	t       preferences.Toggles
	profile settings.ProfileFields
	err     error
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

func (f *fakeSettings) SetRecognitionLoad(_ context.Context, l shared.RecognitionLoad) (preferences.Toggles, error) {
	if f.err != nil {
		return preferences.Toggles{}, f.err
	}
	f.t.RecognitionLoad = l
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
	want := `{"recordingPromptsEnabled":true,"processOnlyOnAcPower":true,"recognitionLoad":"",` +
		`"loadEstimates":null,"loadEstimatesModel":""}`
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
	b, err := s.OpenServices(context.Background())
	if err != nil || b.Settings == nil || b.Activity == nil || b.Prompt == nil || b.Popup == nil || b.Schedule == nil || b.Models == nil || b.Speech == nil ||
		b.Connections == nil || b.Diagnostics == nil || b.Homework == nil || b.Transcript == nil || b.Summary == nil ||
		b.Deletion == nil || b.Knowledge == nil {
		t.Fatalf("OpenServices = %+v, %v", b, err)
	}

	boom := errors.New("no disk")
	opts := validOptions()
	opts.OpenCore = func(context.Context) (Core, error) { return Core{}, boom }
	s, _ = New(opts)
	if _, err := s.OpenServices(context.Background()); !errors.Is(err, boom) {
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
		{name: "post-processing retrier", edit: func(c *Core) { c.Retrier = nil }},
		{name: "recording prompt use case", edit: func(c *Core) { c.Prompt = nil }},
		{name: "schedule import use case", edit: func(c *Core) { c.Schedule = nil }},
		{name: "schedule file reader", edit: func(c *Core) { c.Files = nil }},
		{name: "model catalog", edit: func(c *Core) { c.Models = nil }},
		{name: "model downloads", edit: func(c *Core) { c.Downloads = nil }},
		{name: "model imports", edit: func(c *Core) { c.Imports = nil }},
		{name: "link opener", edit: func(c *Core) { c.Links = nil }},
		{name: "speech component use case", edit: func(c *Core) { c.Speech = nil }},
		{name: "connections use case", edit: func(c *Core) { c.Connections = nil }},
		{name: "google sign-in use case", edit: func(c *Core) { c.Google = nil }},
		{name: "calendar sync use case", edit: func(c *Core) { c.Calendar = nil }},
		{name: "sound check use case", edit: func(c *Core) { c.Sound = nil }},
		{name: "support bundle use case", edit: func(c *Core) { c.Bundle = nil }},
		{name: "homework proposals", edit: func(c *Core) { c.Homework.Proposals = nil }},
		{name: "homework drafts", edit: func(c *Core) { c.Homework.Drafts = nil }},
		{name: "homework deliveries", edit: func(c *Core) { c.Homework.Deliveries = nil }},
		{name: "transcript viewer", edit: func(c *Core) { c.Transcript = nil }},
		{name: "summary viewer", edit: func(c *Core) { c.Summary = nil }},
		{name: "recording deletion", edit: func(c *Core) { c.Deletion = nil }},
		{name: "knowledge base", edit: func(c *Core) { c.Knowledge = nil }},
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
			if _, err := s.OpenServices(context.Background()); err == nil {
				t.Errorf("a core without the %s must fail", tt.name)
			}
		})
	}
}

// TestFrontendKnowsSettingsErrors keeps the error texts the settings page shows as they are equal to the binding's.
func TestFrontendKnowsSettingsErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/settings.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"`+ErrProfileInvalid.Error()+`"`) {
		t.Errorf("frontend/src/bindings/settings.ts does not list the error %q", ErrProfileInvalid)
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
		{file: "settings.ts", typ: reflect.TypeFor[*SettingsService](), methods: 6}, // SetRecognitionLoad: TASK-084, +GetProfile, SaveProfile (TASK-094)
		{file: "activity.ts", typ: reflect.TypeFor[*ActivityService](), methods: 8}, // +SwitchSource, DismissAdvice (TASK-082), +RetryPostProcessing, RedoSummary (TASK-086)
		{file: "prompt.ts", typ: reflect.TypeFor[*PromptService](), methods: 7},     // -Snooze (TASK-076), +2 (TASK-081), +Advice (TASK-082)
		{file: "prompt.ts", typ: reflect.TypeFor[*PopupWindowService](), methods: 1},
		{file: "schedule.ts", typ: reflect.TypeFor[*ScheduleImportService](), methods: 3},
		{file: "models.ts", typ: reflect.TypeFor[*ModelsService](), methods: 13},
		{file: "speech.ts", typ: reflect.TypeFor[*SpeechService](), methods: 3},
		{file: "connections.ts", typ: reflect.TypeFor[*ConnectionsService](), methods: 9},
		{file: "google.ts", typ: reflect.TypeFor[*GoogleService](), methods: 6},
		{file: "calendar.ts", typ: reflect.TypeFor[*GoogleCalendarService](), methods: 2},
		{file: "diagnostics.ts", typ: reflect.TypeFor[*DiagnosticsService](), methods: 4},
		{file: "help.ts", typ: reflect.TypeFor[*HelpService](), methods: 1},
		{file: "homework.ts", typ: reflect.TypeFor[*HomeworkService](), methods: 17}, // +CorrectLesson (TASK-092), +DraftStatus, ExportDocx, SaveDocx (TASK-094)
		{file: "transcript.ts", typ: reflect.TypeFor[*TranscriptService](), methods: 6},
		{file: "summary.ts", typ: reflect.TypeFor[*SummaryService](), methods: 3},                 // TASK-087
		{file: "summary.ts", typ: reflect.TypeFor[*LectureTextService](), methods: 2},             // TASK-090
		{file: "recordingDelete.ts", typ: reflect.TypeFor[*RecordingDeleteService](), methods: 2}, // TASK-095
		{file: "knowledgeBase.ts", typ: reflect.TypeFor[*KnowledgeBaseService](), methods: 6},     // TASK-091
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

func (f *fakeSettings) Profile(context.Context) (settings.ProfileFields, error) {
	return f.profile, f.err
}

func (f *fakeSettings) SaveProfile(_ context.Context, p settings.ProfileFields) (settings.ProfileFields, error) {
	if f.err != nil {
		return settings.ProfileFields{}, f.err
	}
	v, err := settings.NewProfile(p)
	if err != nil {
		return settings.ProfileFields{}, err
	}
	f.profile = v.Fields()
	return f.profile, nil
}

// TASK-094: the profile of the settings page, its JSON names and its errors.
func TestSettingsServiceProfile(t *testing.T) {
	ctx := context.Background()
	f := &fakeSettings{}
	s := NewSettingsService(f, discard())

	if got, err := s.GetProfile(ctx); err != nil || got != (StudentProfile{}) {
		t.Fatalf("GetProfile = %+v, %v", got, err)
	}
	got, err := s.SaveProfile(ctx, StudentProfile{FullName: " Иванов Иван ", Group: "ИВТ-21", University: "МГУ", Faculty: "ВМК", City: "Москва"})
	want := StudentProfile{FullName: "Иванов Иван", Group: "ИВТ-21", University: "МГУ", Faculty: "ВМК", City: "Москва"}
	if err != nil || got != want {
		t.Fatalf("SaveProfile = %+v, %v", got, err)
	}
	if read, err := s.GetProfile(ctx); err != nil || read != want {
		t.Fatalf("GetProfile = %+v, %v", read, err)
	}
	if _, err := s.SaveProfile(ctx, StudentProfile{Group: strings.Repeat("я", 201)}); !errors.Is(err, ErrProfileInvalid) {
		t.Fatalf("too long: err = %v, want ErrProfileInvalid", err)
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"fullName":"Иванов Иван","group":"ИВТ-21","university":"МГУ","faculty":"ВМК","city":"Москва"}` {
		t.Fatalf("JSON = %s", raw)
	}

	var logs bytes.Buffer
	failing := NewSettingsService(&fakeSettings{err: errors.New(`open C:\Users\student\db: locked`)}, slog.New(slog.NewTextHandler(&logs, nil)))
	if _, err := failing.GetProfile(ctx); !errors.Is(err, ErrSettingsUnavailable) {
		t.Fatalf("GetProfile: err = %v", err)
	}
	if _, err := failing.SaveProfile(ctx, want); !errors.Is(err, ErrSettingsUnavailable) {
		t.Fatalf("SaveProfile: err = %v", err)
	}
	if !strings.Contains(logs.String(), "locked") {
		t.Fatalf("the cause must be logged, got %q", logs.String())
	}
}
