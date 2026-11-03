package values

import (
	"fmt"
	"slices"
)

// Results of the wave-10 control tools (ADR-0033 §10): get_settings, quota_status and recording. set_setting answers
// with WriteResult. The planner sees enums, a share and a date only: no key, no amount, no path, no app name.

// SettingRow is one setting as get_settings shows it.
type SettingRow struct {
	Setting Setting
	Value   string
}

// SettingsResult is the result of get_settings: each setting once with a value of its own.
func SettingsResult(callID string, rows []SettingRow) (ToolResult, error) {
	body := make(map[string]string, len(rows))
	for _, r := range rows {
		if !ValidSettingValue(r.Setting, r.Value) {
			return ToolResult{}, fmt.Errorf("%w: not a value of setting %s", ErrInvalidResult, r.Setting)
		}
		if _, dup := body[string(r.Setting)]; dup {
			return ToolResult{}, fmt.Errorf("%w: setting %s twice", ErrInvalidResult, r.Setting)
		}
		body[string(r.Setting)] = r.Value
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(map[string]any{"settings": body})}
	if err := res.Validate(); err != nil {
		return ToolResult{}, err
	}
	return res, nil
}

// QuotaRow is what quota_status shows: the remaining share of the quota in percent and the day it renews (zero: it
// does not renew).
type QuotaRow struct {
	RemainingPercent int
	Resets           Date
}

// QuotaResult is the result of quota_status.
func QuotaResult(callID string, q QuotaRow) (ToolResult, error) {
	if q.RemainingPercent < 0 || q.RemainingPercent > 100 {
		return ToolResult{}, fmt.Errorf("%w: remaining share outside 0..100", ErrInvalidResult)
	}
	body := map[string]any{"remaining_percent": q.RemainingPercent}
	if !q.Resets.IsZero() {
		body["resets"] = q.Resets.String()
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := res.Validate(); err != nil {
		return ToolResult{}, err
	}
	return res, nil
}

// RecordingState is the outcome of recording{start|stop}.
type RecordingState string

// Outcomes.
const (
	RecordingStarted   RecordingState = "started"
	RecordingRunning   RecordingState = "already_recording" // start while one runs: nothing changed
	RecordingStopped   RecordingState = "stopped"
	RecordingNotActive RecordingState = "not_recording" // stop while none runs: nothing changed
)

// RecordingRow is what recording shows: the outcome and the recording it concerns: required when it started or runs,
// none for not_recording, optional for stopped (a replayed stop no longer knows it).
type RecordingRow struct {
	State     RecordingState
	Recording Ref
}

// RecordingResult is the result of recording and the ID it shows.
func RecordingResult(callID string, r RecordingRow) (ToolResult, []Ref, error) {
	states := []RecordingState{RecordingStarted, RecordingRunning, RecordingStopped, RecordingNotActive}
	if !slices.Contains(states, r.State) {
		return ToolResult{}, nil, fmt.Errorf("%w: unknown recording state", ErrInvalidResult)
	}
	if err := wantRef(r.Recording, KindRecording, r.State == RecordingStarted || r.State == RecordingRunning); err != nil {
		return ToolResult{}, nil, err
	}
	if r.State == RecordingNotActive && !r.Recording.IsZero() {
		return ToolResult{}, nil, fmt.Errorf("%w: no recording runs", ErrInvalidResult)
	}
	changed := r.State == RecordingStarted || r.State == RecordingStopped
	body := map[string]any{"done": true, "changed": changed, "state": string(r.State)}
	var refs []Ref
	if !r.Recording.IsZero() {
		body["recording"] = r.Recording.ID()
		refs = append(refs, r.Recording)
	}
	res := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := res.Validate(); err != nil {
		return ToolResult{}, nil, err
	}
	return res, refs, nil
}
