package values

import "fmt"

// Results of propose_subjects_from_schedule (ADR-0033 §4). The planner gets how many groups the card lists and
// nothing else: no name, no schedule string, no token, no group or subject ID (ADR-0022). The subjects the student
// creates with «Да» reach it later as subjects of the base.

// ScheduleSubjectsResult is the result of a schedule-subjects card that waits for the student: 1..MaxScheduleGroups
// groups, or 0 when the count is no longer known (a call answered again after the schedule changed); then the result
// has no count.
func ScheduleSubjectsResult(callID string, groups int) (ToolResult, error) {
	if groups < 0 || groups > MaxScheduleGroups {
		return ToolResult{}, fmt.Errorf("%w: a schedule-subjects card lists 0..%d groups", ErrInvalidResult, MaxScheduleGroups)
	}
	body := map[string]any{"proposed": true, "waits_for": "student"}
	if groups > 0 {
		body["groups"] = groups
	}
	r := ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
	if err := r.Validate(); err != nil {
		return ToolResult{}, err
	}
	return r, nil
}

// NothingToCreateResult is the result of propose_subjects_from_schedule when the schedule proposes no subject: no card.
func NothingToCreateResult(callID string) ToolResult {
	return ToolResult{CallID: callID, ResultJSON: `{"nothing_to_create":true}`}
}
