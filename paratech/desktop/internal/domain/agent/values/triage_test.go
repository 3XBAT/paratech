package values

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestTriageResult(t *testing.T) {
	w := mustRef(t, KindWork, "w1")
	res, refs, err := TriageResult("toolu_1", TriageRow{Work: w, Outcome: TriageQuestions, Feasibility: "partial", Questions: 2, Round: 1, Changed: true})
	if err != nil || len(refs) != 1 || refs[0] != w {
		t.Fatalf("TriageResult = %v, %v, %v", res, refs, err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(res.ResultJSON), &body); err != nil {
		t.Fatal(err)
	}
	// Only IDs, enums and counters reach the planner (ADR-0022).
	want := map[string]any{"work": "w1", "outcome": "questions", "feasibility": "partial", "questions": float64(2),
		"round": float64(1), "changed": true}
	if len(body) != len(want) {
		t.Fatalf("body = %v", body)
	}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("body[%s] = %v, want %v", k, body[k], v)
		}
	}

	bad := []TriageRow{
		{Work: mustRef(t, KindAssignment, "a1"), Outcome: TriageReview, Round: 1},
		{Work: w, Outcome: "send_to", Round: 1},
		{Work: w, Outcome: TriageReview, Feasibility: "maybe", Round: 1},
		{Work: w, Outcome: TriageReview, Questions: 1, Round: 1},
		{Work: w, Outcome: TriageQuestions, Round: 1},
		{Work: w, Outcome: TriageQuestions, Questions: 6, Round: 1},
		{Work: w, Outcome: TriageReview, Round: 4},
	}
	for _, r := range bad {
		if _, _, err := TriageResult("toolu_1", r); !errors.Is(err, ErrInvalidResult) {
			t.Errorf("%+v: %v", r, err)
		}
	}
}
