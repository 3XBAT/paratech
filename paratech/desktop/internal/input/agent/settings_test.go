package agent

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// memSettings is the preferences use case of the settings page: the toggles and every call that changed one.
type memSettings struct {
	t      preferences.Toggles
	calls  []string
	getErr error
}

func newMemSettings() *memSettings {
	return &memSettings{t: preferences.Toggles{
		RecordingPromptsEnabled: true, ProcessOnlyOnACPower: false, RecognitionLoad: shared.LoadGentle,
		NoteRecognition: settings.NoteRecognitionLocal,
	}}
}

func (m *memSettings) Get(context.Context) (preferences.Toggles, error) { return m.t, m.getErr }

func (m *memSettings) SetRecordingPromptsEnabled(_ context.Context, on bool) (preferences.Toggles, error) {
	m.calls = append(m.calls, "prompts")
	m.t.RecordingPromptsEnabled = on
	return m.t, nil
}

func (m *memSettings) SetProcessOnlyOnACPower(_ context.Context, on bool) (preferences.Toggles, error) {
	m.calls = append(m.calls, "ac")
	m.t.ProcessOnlyOnACPower = on
	return m.t, nil
}

func (m *memSettings) SetRecognitionLoad(_ context.Context, l shared.RecognitionLoad) (preferences.Toggles, error) {
	m.calls = append(m.calls, "load")
	m.t.RecognitionLoad = l
	return m.t, nil
}

// controlFakes are the use cases of the control tools.
type controlFakes struct {
	settings *memSettings
	quota    *memQuota
	rec      *memRecorder
}

// newControlsWorld is newWorld with the control tools wired into Actions and the journal.
func newControlsWorld(t *testing.T) (*world, *controlFakes) {
	t.Helper()
	w := newWorld(t)
	f := &controlFakes{settings: newMemSettings(), quota: &memQuota{}, rec: newMemRecorder()}
	var err error
	if w.actions, err = NewActions(ActionDeps{
		Library: w.lib, Lessons: w.lessons, Assignments: w.assignments, NewAssignments: w.added, Rerunner: w.jobs,
		Jobs: w.activity, Canceller: w.jobs, Tx: w.tx, Clock: fixedClock{},
		Controls: Controls{Settings: f.settings, Quota: f.quota, Starts: f.rec, Session: f.rec},
	}); err != nil {
		t.Fatal(err)
	}
	if w.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: w.store, Compensator: w.actions, Resolver: w.actions, Clock: fixedClock{}, IDs: &seqActionIDs{},
	}); err != nil {
		t.Fatal(err)
	}
	return w, f
}

func setArgs(setting, value string) string {
	return `{"setting":"` + setting + `","value":"` + value + `"}`
}

// get_settings shows the three toggles as enums and nothing else of the settings (no photo recognition, no profile).
func TestGetSettingsShowsOnlyTheToggles(t *testing.T) {
	w, f := newControlsWorld(t)
	out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolGetSettings, `{}`, actionpolicy.ClassR))
	want := `{"settings":{"ac_power_only":"off","recognition_load":"gentle","recording_prompts":"on"}}`
	if out.Result.IsError || out.Result.ResultJSON != want || len(out.Seen) != 0 {
		t.Fatalf("result = %+v", out)
	}
	if strings.Contains(out.Result.ResultJSON, "local") || len(f.settings.calls) != 0 {
		t.Fatalf("result shows more than the toggles or changed something: %s, %v", out.Result.ResultJSON, f.settings.calls)
	}

	f.settings.getErr = errors.New("disk")
	if out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c2", values.ToolGetSettings, `{}`, actionpolicy.ClassR)); out.Result.ResultJSON != `{"error":"failed"}` {
		t.Fatalf("a failed read = %s", out.Result.ResultJSON)
	}
}

// set_setting runs the page's method of exactly that toggle, journals the value read before as its Undo
// (RestoreSetting, pinned to the intent) and Undo writes it back through the same method.
func TestSetSettingAndUndo(t *testing.T) {
	tests := []struct {
		setting, value, old string
		method              string
		now                 func(m *memSettings) string
	}{
		{"recording_prompts", "off", "on", "prompts", func(m *memSettings) string { return onOff(m.t.RecordingPromptsEnabled) }},
		{"ac_power_only", "on", "off", "ac", func(m *memSettings) string { return onOff(m.t.ProcessOnlyOnACPower) }},
		{"recognition_load", "max", "gentle", "load", func(m *memSettings) string { return string(m.t.RecognitionLoad) }},
	}
	for _, tt := range tests {
		t.Run(tt.setting, func(t *testing.T) {
			w, f := newControlsWorld(t)
			r, e := run10(t, w, req, call10(t, "c1", values.ToolSetSetting, setArgs(tt.setting, tt.value), actionpolicy.ClassU))
			if r.out.Result.ResultJSON != `{"changed":true,"done":true}` || tt.now(f.settings) != tt.value {
				t.Fatalf("result = %s, value %s", r.out.Result.ResultJSON, tt.now(f.settings))
			}
			want := action.RestoreSetting{Setting: values.Setting(tt.setting), OldValue: tt.old}
			if e.Status != action.StatusDone || !e.CanUndo || e.Compensation != want {
				t.Fatalf("entry = %+v, want DONE with %+v", e, want)
			}
			if len(r.cards) != 1 || r.cards[0] != (values.ActionLine{Action: e.ID}) {
				t.Fatalf("cards = %+v", r.cards)
			}
			if u := undo(t, w, e); u.Status != action.StatusUndone || tt.now(f.settings) != tt.old {
				t.Fatalf("undo = %+v, value %s", u, tt.now(f.settings))
			}
			if !slices.Equal(f.settings.calls, []string{tt.method, tt.method}) {
				t.Fatalf("calls = %v, want only %s", f.settings.calls, tt.method)
			}
		})
	}
}

// The value already set changes nothing: no entry, no call, no «Отменить». The same call again answers from its entry.
func TestSetSettingNoopAndReplay(t *testing.T) {
	w, f := newControlsWorld(t)
	r, e := run10(t, w, req, call10(t, "c1", values.ToolSetSetting, setArgs("recording_prompts", "on"), actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"changed":false,"done":true}` || e.ID != "" || len(f.settings.calls) != 0 || len(r.cards) != 0 {
		t.Fatalf("noop = %s, entry %+v, calls %v", r.out.Result.ResultJSON, e, f.settings.calls)
	}

	c := call10(t, "c2", values.ToolSetSetting, setArgs("ac_power_only", "on"), actionpolicy.ClassU)
	run10(t, w, req, c)
	again, _ := run10(t, w, req, c)
	if again.out.Result.ResultJSON != `{"changed":true,"done":true}` || len(f.settings.calls) != 1 {
		t.Fatalf("replay = %s, calls %v", again.out.Result.ResultJSON, f.settings.calls)
	}
}

// Undo follows the setting only: a change of another setting keeps it; the student's own change of the same setting
// refuses it («Изменено после»); the student having set the old value back is undone without a write.
func TestSetSettingUndoAfterAChange(t *testing.T) {
	tests := []struct {
		name   string
		change func(m *memSettings)
		status action.Status
		load   shared.RecognitionLoad
		calls  int
	}{
		{"another setting changed", func(m *memSettings) { m.t.ProcessOnlyOnACPower = true }, action.StatusUndone, shared.LoadGentle, 2},
		{"the student chose another value", func(m *memSettings) { m.t.RecognitionLoad = shared.LoadNormal }, action.StatusUndoRefused, shared.LoadNormal, 1},
		{"the student set it back", func(m *memSettings) { m.t.RecognitionLoad = shared.LoadGentle }, action.StatusUndone, shared.LoadGentle, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, f := newControlsWorld(t)
			_, e := run10(t, w, req, call10(t, "c1", values.ToolSetSetting, setArgs("recognition_load", "max"), actionpolicy.ClassU))
			tt.change(f.settings)
			u := undo(t, w, e)
			if u.Status != tt.status || f.settings.t.RecognitionLoad != tt.load || len(f.settings.calls) != tt.calls {
				t.Fatalf("undo = %+v, load %s, calls %v", u, f.settings.t.RecognitionLoad, f.settings.calls)
			}
			if tt.status == action.StatusUndoRefused && u.NoUndo != action.NoUndoChanged {
				t.Fatalf("refusal = %q, want «Изменено после»", u.NoUndo)
			}
		})
	}
}

// The agent can change exactly the three toggles: the allowlist is pinned, and any other setting (the removed
// free-work notice, photo recognition, a permission or anything a later catalog adds) reaches no use case.
func TestSetSettingAllowlist(t *testing.T) {
	want := []values.Setting{values.SettingRecordingPrompts, values.SettingACPowerOnly, values.SettingRecognitionLoad}
	if !slices.Equal(agentSettings, want) {
		t.Fatalf("allowlist = %v, want %v: widening it needs its own decision (ADR-0034)", agentSettings, want)
	}
	if n := reflect.TypeFor[AgentSettings]().NumMethod(); n != 4 {
		t.Fatalf("AgentSettings has %d methods, want Get and the three toggles only", n)
	}

	w, f := newControlsWorld(t)
	for _, s := range []values.Setting{"free_work_notice", "note_recognition", "permissions", "hub_url", ""} {
		for _, v := range []string{"on", "off", "local", "max"} {
			if _, err := w.actions.writeSetting(t.Context(), s, v); !errors.Is(err, errSettingNotAllowed) {
				t.Errorf("writeSetting(%q, %q) = %v, want refused", s, v, err)
			}
		}
		if _, err := settingOf(f.settings.t, s); !errors.Is(err, errSettingNotAllowed) {
			t.Errorf("settingOf(%q) = %v, want refused", s, err)
		}
	}
	if len(f.settings.calls) != 0 {
		t.Fatalf("an unknown setting reached the use case: %v", f.settings.calls)
	}
	// A value of another setting is refused before any call too.
	if _, err := w.actions.writeSetting(t.Context(), values.SettingRecognitionLoad, "on"); !errors.Is(err, errSettingValue) || len(f.settings.calls) != 0 {
		t.Fatalf("recognition_load on = %v, calls %v", err, f.settings.calls)
	}
	// The model cannot even name them: the call is refused when parsed.
	for _, args := range []string{
		`{"setting":"free_work_notice","value":"off"}`, `{"setting":"note_recognition","value":"local"}`,
		`{"setting":"recognition_load","value":"on"}`,
	} {
		if _, err := values.ParseCallAt("c1", string(values.ToolSetSetting), args, values.CatalogWave10); err == nil {
			t.Errorf("%s parsed", args)
		}
	}
}

func TestSetSettingRecover(t *testing.T) {
	t.Run("set, DONE not recorded", func(t *testing.T) {
		w, f := newControlsWorld(t)
		crash(t, w, call10(t, "c1", values.ToolSetSetting, setArgs("ac_power_only", "on"), actionpolicy.ClassU))
		e := recovered(t, w, "c1")
		want := action.RestoreSetting{Setting: values.SettingACPowerOnly, OldValue: "off"}
		if e.Status != action.StatusDone || e.Compensation != want {
			t.Fatalf("recovered = %+v, want DONE with %+v", e, want)
		}
		if u := undo(t, w, e); u.Status != action.StatusUndone || f.settings.t.ProcessOnlyOnACPower {
			t.Fatalf("undo after recover = %+v", u)
		}
	})
	t.Run("not set: FAILED", func(t *testing.T) {
		w, _ := newControlsWorld(t)
		p, _, err := w.actions.prepare(t.Context(), originOf(req), call10(t, "c1", values.ToolSetSetting, setArgs("ac_power_only", "on"), actionpolicy.ClassU).Call)
		if err != nil {
			t.Fatal(err)
		}
		begin(t, w, "c1", values.ToolSetSetting, p.intent)
		if e := recovered(t, w, "c1"); e.Status != action.StatusFailed {
			t.Fatalf("recovered = %+v", e)
		}
	})
}

// Without the use case the setting tools answer "unavailable".
func TestSettingsUnwired(t *testing.T) {
	w := newWorld(t)
	out := w.tools(t).read(t.Context(), values.Scope{}, call10(t, "c1", values.ToolGetSettings, `{}`, actionpolicy.ClassR))
	if out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("get_settings = %s", out.Result.ResultJSON)
	}
	r, _ := run10(t, w, req, call10(t, "c2", values.ToolSetSetting, setArgs("ac_power_only", "on"), actionpolicy.ClassU))
	if r.out.Result.ResultJSON != `{"error":"unavailable"}` {
		t.Fatalf("set_setting = %s", r.out.Result.ResultJSON)
	}
}
