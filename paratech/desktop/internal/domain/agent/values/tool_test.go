package values

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestRetiredTool: generate_draft left the catalog in v4. The planner cannot call or plan it, but a stored plan, open
// call or action that names it still restores (TASK-184).
func TestRetiredTool(t *testing.T) {
	if CatalogVersion != 9 {
		t.Fatalf("CatalogVersion = %d", CatalogVersion)
	}
	if slices.Contains(Tools(), ToolGenerateDraft) || !slices.Equal(RetiredTools(), []Tool{ToolGenerateDraft}) {
		t.Fatalf("catalog %v, retired %v", Tools(), RetiredTools())
	}
	if _, err := ParseTool("generate_draft"); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ParseTool(generate_draft) = %v", err)
	}
	if _, err := SpecOf(ToolGenerateDraft); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("SpecOf(generate_draft) = %v", err)
	}
	if _, err := ParseCall("c1", "generate_draft", `{"assignment":"a1"}`); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("ParseCall(generate_draft) = %v", err)
	}
	if !ToolGenerateDraft.Valid() || Tool("send_email").Valid() {
		t.Fatal("Valid must know the retired tool and nothing else")
	}

	stored := `[{"tool":"generate_draft","arguments":{"assignment":"a1"}},{"tool":"edit_due","arguments":{"assignment":"a1","due":"2026-11-03"}}]`
	steps, err := DecodePlanSteps(stored)
	if err != nil {
		t.Fatalf("DecodePlanSteps(stored v3 plan) = %v", err)
	}
	if steps[0].Tool() != ToolGenerateDraft || (PlanCard{Summary: "План", Steps: steps}).Validate() != nil {
		t.Fatalf("restored steps = %+v", steps)
	}
	if enc, err := EncodePlanSteps(steps); err != nil || enc != stored {
		t.Fatalf("EncodePlanSteps = %s, %v", enc, err)
	}
	// The stored schema still holds: a retired step keeps its argument rules.
	for _, bad := range []string{
		`[{"tool":"generate_draft"}]`,
		`[{"tool":"generate_draft","arguments":{"assignment":"a1","path":"C:/x"}}]`,
	} {
		if _, err := DecodePlanSteps(bad); !errors.Is(err, ErrInvalidArguments) {
			t.Errorf("DecodePlanSteps(%s) = %v", bad, err)
		}
	}
	if _, err := ParseCall("c1", "show_plan", `{"summary":"План","steps":`+stored+`}`); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("the planner planned a retired tool: %v", err)
	}
}

// TestTriageTopic: triage_assignment takes a topic (catalog v4), a query of at most MaxQueryRunes, as an argument or
// a plan step; it carries no ID.
func TestTriageTopic(t *testing.T) {
	topic := strings.Repeat("я", MaxQueryRunes)
	call, err := ParseCall("c1", "triage_assignment", `{"topic":"  `+topic+`  "}`)
	if err != nil {
		t.Fatal(err)
	}
	if q, ok := call.Args().Query("topic"); !ok || string(q) != topic || len(call.Refs()) != 0 {
		t.Fatalf("topic = %q, %v; refs %v", q, ok, call.Refs())
	}

	spec, err := SpecOf(ToolTriageAssignment)
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.OneOf(); len(got) != 1 || !slices.Equal(got[0], []string{"assignment", "upload", "topic"}) {
		t.Fatalf("one-of = %v", got)
	}
	spec.OneOf()[0][0] = "x"
	if again, _ := SpecOf(ToolTriageAssignment); again.OneOf()[0][0] != "assignment" {
		t.Fatal("OneOf returns the catalog's own slice")
	}

	steps, err := DecodePlanSteps(`[{"tool":"triage_assignment","arguments":{"topic":"Реформы Петра I"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Matches(call) || len(steps[0].Deferred()) != 0 {
		t.Fatal("a planned topic matched another topic or deferred an id")
	}
	same, _ := ParseCall("c2", "triage_assignment", `{"topic":"Реформы Петра I"}`)
	if !steps[0].Matches(same) {
		t.Fatal("the planned topic does not match its call")
	}
}
