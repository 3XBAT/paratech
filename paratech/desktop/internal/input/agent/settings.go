package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// get_settings and set_setting (catalog v10, ADR-0033 §10) on the use case of the settings page. The agent reads and
// changes exactly the three toggles below (agentSettings), each through its own method; it never reaches the profile,
// the photo recognition, the connections or any permission (ADR-0034 «Разрешения»): AgentSettings has no method for
// them and every switch below refuses a setting it does not name.

// Controls are the use cases of the catalog-v10 control tools; each nil one makes its tools answer "unavailable".
type Controls struct {
	Settings AgentSettings    // get_settings, set_setting
	Quota    QuotaStatus      // quota_status
	Starts   RecordingStarts  // recording{start}
	Session  RecordingSession // recording{start|stop} and the Undo of a start
}

// AgentSettings is the preferences use case (cases/preferences) as the agent uses it: the methods of the settings page
// (input/wails SettingsService) for the three toggles, and nothing else.
type AgentSettings interface {
	Get(ctx context.Context) (preferences.Toggles, error)
	SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetRecognitionLoad(ctx context.Context, load shared.RecognitionLoad) (preferences.Toggles, error)
}

// agentSettings is the allowlist of the settings the agent reads and changes, in the order get_settings shows them. A
// setting values gains later (wave 11) is not reachable until it is added here and to settingOf and writeSetting.
var agentSettings = []values.Setting{
	values.SettingRecordingPrompts, values.SettingACPowerOnly, values.SettingRecognitionLoad,
}

var (
	// errSettingNotAllowed: the setting is not one the agent may read or change.
	errSettingNotAllowed = errors.New("agent: the agent may not change this setting")
	// errSettingValue: the value is not one of the setting.
	errSettingValue = errors.New("agent: not a value of the setting")
)

// settingOf is the value of setting s in t; a setting outside the allowlist is refused.
func settingOf(t preferences.Toggles, s values.Setting) (string, error) {
	var v string
	switch s {
	case values.SettingRecordingPrompts:
		v = onOff(t.RecordingPromptsEnabled)
	case values.SettingACPowerOnly:
		v = onOff(t.ProcessOnlyOnACPower)
	case values.SettingRecognitionLoad:
		v = t.RecognitionLoad.OrDefault().String()
	default:
		return "", fmt.Errorf("%w: %s", errSettingNotAllowed, s)
	}
	if !slices.Contains(agentSettings, s) || !values.ValidSettingValue(s, v) {
		return "", fmt.Errorf("%w: %s", errSettingNotAllowed, s)
	}
	return v, nil
}

func onOff(on bool) string {
	if on {
		return values.SettingOn
	}
	return values.SettingOff
}

// writeSetting sets s to v through the page's method of that one toggle; any other setting reaches no use case.
func (a *Actions) writeSetting(ctx context.Context, s values.Setting, v string) (preferences.Toggles, error) {
	uc := a.controls.Settings
	switch {
	case uc == nil:
		return preferences.Toggles{}, errUnavailable
	case !slices.Contains(agentSettings, s):
		return preferences.Toggles{}, fmt.Errorf("%w: %s", errSettingNotAllowed, s)
	case !values.ValidSettingValue(s, v):
		return preferences.Toggles{}, fmt.Errorf("%w: %s", errSettingValue, s)
	}
	switch s {
	case values.SettingRecordingPrompts:
		return uc.SetRecordingPromptsEnabled(ctx, v == values.SettingOn)
	case values.SettingACPowerOnly:
		return uc.SetProcessOnlyOnACPower(ctx, v == values.SettingOn)
	case values.SettingRecognitionLoad:
		return uc.SetRecognitionLoad(ctx, shared.RecognitionLoad(v))
	}
	return preferences.Toggles{}, fmt.Errorf("%w: %s", errSettingNotAllowed, s)
}

// settingVersion is the version of setting s as a target: the position of its value v (1..), 0 for none. Each setting
// has its own, so a change of another one or of anything else stored in the settings (the remembered recording
// source, the default model) does not refuse an Undo; setting it back and forth gives the same version, which is the
// same state.
func settingVersion(s values.Setting, v string) int64 {
	return int64(slices.Index(values.SettingValues(s), v) + 1)
}

// readSetting reads the current value of s.
func (a *Actions) readSetting(ctx context.Context, s values.Setting) (string, error) {
	if a.controls.Settings == nil {
		return "", errUnavailable
	}
	t, err := a.controls.Settings.Get(ctx)
	if err != nil {
		return "", err
	}
	return settingOf(t, s)
}

// setSetting is a toggle of «Настройки»: the value read before is the Undo (RestoreSetting, the intent's Prior); the
// value already set changes nothing and journals nothing.
func (a *Actions) setSetting(ctx context.Context, call values.Call) (prepared, error) {
	if a.controls.Settings == nil {
		return prepared{}, errUnavailable
	}
	name, _ := call.Args().Enum("setting")
	value, _ := call.Args().Enum("value")
	s := values.Setting(name)
	old, err := a.readSetting(ctx, s)
	if err != nil {
		return prepared{}, err
	}
	if !values.ValidSettingValue(s, value) {
		return prepared{}, fmt.Errorf("%w: %s", errSettingValue, s)
	}
	if old == value {
		return prepared{noop: &values.WriteRow{}}, nil
	}
	undo := action.RestoreSetting{Setting: s, OldValue: old}
	return prepared{
		intent: action.Intent{Prior: undo, Next: action.RestoreSetting{Setting: s, OldValue: value}},
		run: func(ctx context.Context) (effect, error) {
			var e effect
			err := a.tx.Do(ctx, func(ctx context.Context) error {
				cur, err := a.readSetting(ctx, s)
				if err != nil {
					return err
				}
				if cur != old {
					return errMovedOn
				}
				t, err := a.writeSetting(ctx, s, value)
				if err != nil {
					return err
				}
				after, err := settingOf(t, s)
				if err != nil {
					return err
				}
				e = effect{
					comp: undo, version: settingVersion(s, after), row: values.WriteRow{Changed: true},
					line: values.LineResult{Kind: values.LineSetting, Setting: s, Value: after},
				}
				return nil
			})
			return e, err
		},
	}, nil
}

// checkSetting reads the target of a RestoreSetting: compensated when the setting holds the old value again.
func (a *Actions) checkSetting(ctx context.Context, c action.RestoreSetting) (action.Target, error) {
	cur, err := a.readSetting(ctx, c.Setting)
	if err != nil {
		return action.Target{}, err
	}
	return action.Target{Version: settingVersion(c.Setting, cur), Compensated: cur == c.OldValue}, nil
}

// observeSetting settles a PENDING set_setting after a crash: the setting holds the value the call set (the intent's
// Next) -> DONE with the prior one; else FAILED (a setting has no target ID, so Settle reads any other state so).
func (a *Actions) observeSetting(ctx context.Context, in action.Intent) (action.Observation, error) {
	next, ok := in.Next.(action.RestoreSetting)
	if !ok {
		return action.Observation{}, nil
	}
	cur, err := a.readSetting(ctx, next.Setting)
	if err != nil {
		return action.Observation{}, err
	}
	o := action.Observation{Known: true, Version: settingVersion(next.Setting, cur)}
	if cur == next.OldValue {
		o.Effect, o.Compensation = true, in.Prior
	}
	return o, nil
}

// --- get_settings and the hooks of the control tools -----------------------------------------------------------

// getSettings shows the three toggles as enums; nothing else of the settings reaches the planner.
func (t tools) getSettings(ctx context.Context, call values.Call, _ values.Scope) (values.ToolResult, []values.Ref, error) {
	tg, err := t.actions.controls.Settings.Get(ctx)
	if err != nil {
		return values.ToolResult{}, nil, err
	}
	rows := make([]values.SettingRow, 0, len(agentSettings))
	for _, s := range agentSettings {
		v, err := settingOf(tg, s)
		if err != nil {
			return values.ToolResult{}, nil, err
		}
		rows = append(rows, values.SettingRow{Setting: s, Value: v})
	}
	res, err := values.SettingsResult(call.ID(), rows)
	return res, nil, err
}

// controlReader is the reader of the control read tools (get_settings, quota_status) when their use case is wired.
func (t tools) controlReader(tool values.Tool) (read, bool) {
	if t.actions == nil {
		return nil, false
	}
	switch tool {
	case values.ToolGetSettings:
		return t.getSettings, t.actions.controls.Settings != nil
	case values.ToolQuotaStatus:
		return t.quotaStatus, t.actions.controls.Quota != nil
	}
	return nil, false
}

// resultOf builds the result of a tool with its own result shape (recording) and adds cards.
type resultOf func(t tools, c agentturn.Call, cards []values.Card) ran

// replayControl rebuilds the result of a recording call from its entry, never running it against the state of now;
// set_setting answers as every write. A FAILED stop found no recording running (not_recording); a DONE start without
// a compensation found one running (already_recording). Its ID is not in the entry, so the replay names the one that
// runs now, or answers not_recording when the student stopped it since: still nothing starts.
func (t tools) replayControl(c agentturn.Call, e agentjournal.Entry) (ran, bool) {
	if e.Tool != values.ToolRecording {
		return ran{}, false
	}
	act, _ := c.Call.Args().Enum(values.ArgAction)
	notRecording := recordingOut(values.RecordingNotActive, values.Ref{})
	switch e.Status {
	case action.StatusFailed:
		if act == values.RecordingStop {
			return notRecording(t, c, nil), true
		}
		return ran{}, false
	case action.StatusPending, action.StatusUnknown, action.StatusProposed:
		return ran{}, false
	}
	if v, ok := e.Compensation.(action.StopRecording); ok {
		return recordingOut(values.RecordingStarted, v.Recording)(t, c, nil), true
	}
	if act != values.RecordingStart {
		return recordingOut(values.RecordingStopped, values.Ref{})(t, c, nil), true
	}
	if s := t.actions.controls.Session; s != nil {
		if info, ok := s.Active(); ok {
			if r, err := recordingRefOf(info.ID); err == nil {
				return recordingOut(values.RecordingRunning, r)(t, c, nil), true
			}
		}
	}
	return notRecording(t, c, nil), true
}

// controlRefusal maps the errors of the control tools the generic map does not know.
func controlRefusal(tool values.Tool, err error) (values.Refusal, bool) {
	switch tool {
	case values.ToolSetSetting:
		if errors.Is(err, errSettingNotAllowed) || errors.Is(err, errSettingValue) {
			return values.RefuseInvalidArgs, true
		}
	case values.ToolRecording:
		return recordingRefusal(err)
	}
	return "", false
}
