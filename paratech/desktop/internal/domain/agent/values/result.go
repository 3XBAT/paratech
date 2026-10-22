package values

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of the history sent to the Hub (gateway.proto AgentTurn, ADR-0023).
const (
	MaxResultBytes      = 16 << 10 // result_json of one call
	MaxStudentTextBytes = 4 << 10  // student_text of one request
	MaxUploads          = 10       // uploads attached to one request
)

// Validate checks a result before it is recorded: a well-formed call ID and a JSON value of at most MaxResultBytes.
func (r ToolResult) Validate() error {
	if err := checkCallID(r.CallID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResult, err)
	}
	if r.ResultJSON == "" || len(r.ResultJSON) > MaxResultBytes || !json.Valid([]byte(r.ResultJSON)) {
		return fmt.Errorf("%w: result must be JSON of 1..%d bytes", ErrInvalidResult, MaxResultBytes)
	}
	return nil
}

// Refusal is the error enum of a call the desktop did not run (ADR-0022: error texts never reach the planner).
type Refusal string

// Refusals.
const (
	RefuseUnknownTool   Refusal = "unknown_tool"
	RefuseInvalidArgs   Refusal = "invalid_arguments"
	RefuseUnknownID     Refusal = "unknown_id"
	RefusePlanRequired  Refusal = "plan_required"
	RefuseNotInPlan     Refusal = "not_in_plan"
	RefuseInvalidPlan   Refusal = "invalid_plan"
	RefuseTooManyRounds Refusal = "too_many_rounds"
	RefuseBudget        Refusal = "budget"
	RefuseNotNow        Refusal = "not_now"  // the request waits for the student or has ended
	RefuseNotDone       Refusal = "not_done" // cancelled or interrupted before it ran
	RefuseFailed        Refusal = "failed"   // the use case failed
)

// assumptionsNote tells the planner to go on after a refused third question round (ADR-0021).
const assumptionsNote = "continue_with_stated_assumptions"

// RefusalResult is the is_error result of a call that was not run.
func RefusalResult(callID string, why Refusal) ToolResult {
	body := map[string]string{"error": string(why)}
	if why == RefuseTooManyRounds {
		body["note"] = assumptionsNote
	}
	return ToolResult{CallID: callID, ResultJSON: mustJSON(body), IsError: true}
}

// ShownResult is the result of a card tool the student has seen (reply, cannot_do).
func ShownResult(callID string) ToolResult {
	return ToolResult{CallID: callID, ResultJSON: `{"shown":true}`}
}

// PlanResult is the result of show_plan: approved, or a change the student asked for in their own words.
func PlanResult(callID string, approved bool, change Label) ToolResult {
	body := map[string]any{"approved": approved}
	if change != "" {
		body["change"] = string(change)
	}
	return ToolResult{CallID: callID, ResultJSON: mustJSON(body)}
}

// Answer is the student's answer to one question of a Questions card: an option index or their own text, never the
// text of the question or the option (ADR-0022).
type Answer struct {
	Question int
	Option   int  // choice: index of the picked option
	Text     Text // text question: what the student typed; empty for a choice
}

// AnswersResult is the result of ask_questions.
func AnswersResult(callID string, answers []Answer) (ToolResult, error) {
	if len(answers) == 0 || len(answers) > MaxQuestions {
		return ToolResult{}, fmt.Errorf("%w: 1..%d answers", ErrInvalidResult, MaxQuestions)
	}
	type wire struct {
		Question int     `json:"question"`
		Option   *int    `json:"option,omitempty"`
		Text     *string `json:"text,omitempty"`
	}
	seen := map[int]bool{}
	out := make([]wire, 0, len(answers))
	for _, a := range answers {
		if a.Question < 0 || a.Question >= MaxQuestions || seen[a.Question] {
			return ToolResult{}, fmt.Errorf("%w: question index out of range or repeated", ErrInvalidResult)
		}
		seen[a.Question] = true
		w := wire{Question: a.Question}
		if a.Text != "" {
			t, err := ParseText(string(a.Text))
			if err != nil {
				return ToolResult{}, fmt.Errorf("%w: answer: %w", ErrInvalidResult, err)
			}
			s := string(t)
			w.Text = &s
		} else {
			if a.Option < 0 || a.Option >= MaxOptions {
				return ToolResult{}, fmt.Errorf("%w: option index out of range", ErrInvalidResult)
			}
			o := a.Option
			w.Option = &o
		}
		out = append(out, w)
	}
	return ToolResult{CallID: callID, ResultJSON: mustJSON(map[string]any{"answers": out})}, nil
}

// mustJSON encodes the fixed shapes above (maps and structs of strings, bools and ints), which cannot fail.
func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// NewStudentText is the student turn of a request: the typed text and, on a line of its own, the IDs of the attached
// uploads, so the planner can name them in arguments. At most MaxStudentTextBytes, uploads of kind upload only.
func NewStudentText(text string, uploads []Ref) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" && len(uploads) == 0 {
		return "", fmt.Errorf("%w: an empty request", ErrInvalidText)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("%w: request text is not valid UTF-8", ErrInvalidText)
	}
	if len(uploads) > MaxUploads {
		return "", fmt.Errorf("%w: more than %d uploads", ErrInvalidText, MaxUploads)
	}
	ids := make([]string, 0, len(uploads))
	seen := RefSet{}
	for _, u := range uploads {
		if u.Kind() != KindUpload || seen.Has(u) {
			return "", fmt.Errorf("%w: uploads must be distinct upload ids", ErrInvalidText)
		}
		seen.Add(u)
		ids = append(ids, u.String())
	}
	if len(ids) > 0 {
		if text != "" {
			text += "\n"
		}
		text += "[" + strings.Join(ids, " ") + "]"
	}
	if len(text) > MaxStudentTextBytes {
		return "", fmt.Errorf("%w: request text is longer than %d bytes", ErrInvalidText, MaxStudentTextBytes)
	}
	return text, nil
}

// TimeZoneName returns an IANA-shaped name of the zone of t for the base: the location name when it is one (not
// "Local"), else Etc/GMT±h for a whole-hour offset (the sign is inverted in that scheme), else UTC.
func TimeZoneName(t time.Time) string {
	if name := t.Location().String(); name != "Local" && validTimeZone(name) {
		return name
	}
	_, offset := t.Zone()
	if offset == 0 {
		return "UTC"
	}
	if offset%3600 != 0 {
		return "UTC"
	}
	h := -offset / 3600
	if h > 0 {
		return fmt.Sprintf("Etc/GMT+%d", h)
	}
	return fmt.Sprintf("Etc/GMT%d", h)
}
