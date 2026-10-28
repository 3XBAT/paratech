package values

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRefusalResult(t *testing.T) {
	r := RefusalResult("c1", RefuseTooManyRounds)
	if !r.IsError || r.ResultJSON != `{"error":"too_many_rounds","note":"continue_with_stated_assumptions"}` {
		t.Fatalf("third round refusal = %+v", r)
	}
	if r := RefusalResult("c1", RefuseUnknownID); r.ResultJSON != `{"error":"unknown_id"}` || r.Validate() != nil {
		t.Fatalf("refusal = %+v", r)
	}
	if r := PlanResult("c2", false, "без лабораторных"); r.ResultJSON != `{"approved":false,"change":"без лабораторных"}` {
		t.Fatalf("plan result = %s", r.ResultJSON)
	}
	if r := ShownResult("c3"); r.Validate() != nil || r.IsError {
		t.Fatalf("shown = %+v", r)
	}
}

func TestToolResultValidate(t *testing.T) {
	tests := []struct {
		name string
		r    ToolResult
		ok   bool
	}{
		{"object", ToolResult{CallID: "c1", ResultJSON: `{"a":1}`}, true},
		{"no call id", ToolResult{ResultJSON: `{}`}, false},
		{"bad call id", ToolResult{CallID: "c 1", ResultJSON: `{}`}, false},
		{"not JSON", ToolResult{CallID: "c1", ResultJSON: `{`}, false},
		{"empty", ToolResult{CallID: "c1"}, false},
		{"too large", ToolResult{CallID: "c1", ResultJSON: `"` + strings.Repeat("a", MaxResultBytes) + `"`}, false},
	}
	for _, tt := range tests {
		if err := tt.r.Validate(); (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalidResult)) {
			t.Errorf("%s: Validate = %v", tt.name, err)
		}
	}
}

func TestAnswersResult(t *testing.T) {
	r, err := AnswersResult("c1", []Answer{{Question: 0, Option: 1}, {Question: 1, Text: "к пятнице"}})
	if err != nil || r.ResultJSON != `{"answers":[{"question":0,"option":1},{"question":1,"text":"к пятнице"}]}` {
		t.Fatalf("answers = %s, %v", r.ResultJSON, err)
	}
	bad := [][]Answer{
		nil,
		{{Question: 0, Option: MaxOptions}},
		{{Question: 0, Option: NoDefault}}, // a choice with nothing preselected is never sent unanswered (TASK-234)
		{{Question: MaxQuestions}},
		{{Question: 0}, {Question: 0}},
		{{Question: 0, Text: "<b>"}},
	}
	for i, a := range bad {
		if _, err := AnswersResult("c1", a); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("bad answers %d = %v", i, err)
		}
	}
}

func TestNewStudentText(t *testing.T) {
	u1, _ := NewRef(KindUpload, "u1")
	u2, _ := NewRef(KindUpload, "u2")
	s1, _ := NewRef(KindSubject, "s1")
	got, err := NewStudentText("  разложи файлы ", []Ref{u1, u2})
	if err != nil || got != "разложи файлы\n[upload:u1 upload:u2]" {
		t.Fatalf("text = %q, %v", got, err)
	}
	if got, err := NewStudentText("", []Ref{u1}); err != nil || got != "[upload:u1]" {
		t.Fatalf("uploads only = %q, %v", got, err)
	}
	for name, tc := range map[string]struct {
		text    string
		uploads []Ref
	}{
		"empty":       {" ", nil},
		"not upload":  {"x", []Ref{s1}},
		"duplicate":   {"x", []Ref{u1, u1}},
		"too long":    {strings.Repeat("я", MaxStudentTextBytes), nil},
		"invalid utf": {"\xff", nil},
	} {
		if _, err := NewStudentText(tc.text, tc.uploads); !errors.Is(err, ErrInvalidText) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTimeZoneName(t *testing.T) {
	tests := []struct {
		loc  *time.Location
		want string
	}{
		{time.FixedZone("Local", 3*3600), "Etc/GMT-3"},
		{time.FixedZone("Local", -5*3600), "Etc/GMT+5"},
		{time.FixedZone("Local", 0), "UTC"},
		{time.FixedZone("Local", 5*3600+1800), "UTC"},
		{time.FixedZone("Europe/Moscow", 3*3600), "Europe/Moscow"},
		{time.FixedZone("MSK zone", 3*3600), "Etc/GMT-3"},
	}
	for _, tt := range tests {
		at := time.Date(2026, 10, 22, 12, 0, 0, 0, tt.loc)
		if got := TimeZoneName(at); got != tt.want {
			t.Errorf("TimeZoneName(%s) = %q, want %q", tt.loc, got, tt.want)
		}
	}
}

func TestHistoryTurnValidate(t *testing.T) {
	calls := &SealedCalls{Calls: []RawCall{{CallID: "c1", Tool: "reply", ArgumentsJSON: `{}`}}, Seal: []byte{1}}
	good := []HistoryTurn{
		{StudentText: "привет"},
		{Calls: calls},
		{Results: []ToolResult{{CallID: "c1", ResultJSON: `{}`}}},
		{AgentReply: "reply: горутина — это"},
		{AgentReply: strings.Repeat("a", MaxAgentReplyBytes)},
	}
	for i, h := range good {
		if err := h.Validate(); err != nil {
			t.Errorf("good %d: %v", i, err)
		}
	}
	bad := []HistoryTurn{
		{},
		{StudentText: "x", Calls: calls},
		{Calls: &SealedCalls{Calls: calls.Calls}},
		{Results: []ToolResult{{CallID: "c1"}}},
		{StudentText: strings.Repeat("a", MaxStudentTextBytes+1)},
		{StudentText: "x", AgentReply: "y"},
		{AgentReply: "y", Results: []ToolResult{{CallID: "c1", ResultJSON: `{}`}}},
		{AgentReply: strings.Repeat("a", MaxAgentReplyBytes+1)},
		{AgentReply: " \n "},
		{AgentReply: "a\x00b"},
		{AgentReply: "a\xffb"},
	}
	for i, h := range bad {
		if err := h.Validate(); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("bad %d: %v", i, err)
		}
	}
}
