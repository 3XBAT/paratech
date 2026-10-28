package values

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// subjectSteps returns plan_steps of n create_subject steps with distinct names.
func subjectSteps(n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"tool":"create_subject","arguments":{"name":"Предмет %d"}}`, i)
	}
	return `[` + strings.Join(items, ",") + `]`
}

// padded returns a JSON object of exactly size bytes: body (an object) with spaces before its closing brace.
func padded(body string, size int) string {
	return strings.TrimSuffix(body, "}") + strings.Repeat(" ", size-len(body)) + "}"
}

func TestPlanStepLimit(t *testing.T) {
	plan := `{"summary":"План","steps":` + subjectSteps(MaxPlanSteps) + `}`
	if len(plan) <= MaxArgumentsBytes || len(plan) > MaxPlanArgumentsBytes {
		t.Fatalf("a plan of %d steps is %d bytes", MaxPlanSteps, len(plan))
	}
	c, err := ParseCall("c1", "show_plan", plan)
	if err != nil {
		t.Fatalf("plan of %d steps = %v", MaxPlanSteps, err)
	}

	// The stored form (sqlite ProposedPlan/ApprovedPlan) keeps every step.
	stored, err := EncodePlanSteps(c.Args().PlanSteps("steps"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodePlanSteps(stored)
	if err != nil || len(back) != MaxPlanSteps {
		t.Fatalf("round trip: %d steps, %v", len(back), err)
	}

	over := subjectSteps(MaxPlanSteps + 1)
	if _, err := ParseCall("c1", "show_plan", `{"summary":"План","steps":`+over+`}`); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("plan of %d steps = %v", MaxPlanSteps+1, err)
	}
	if _, err := DecodePlanSteps(over); !errors.Is(err, ErrInvalidArguments) {
		t.Fatalf("stored plan of %d steps = %v", MaxPlanSteps+1, err)
	}
}

func TestArgumentsSizeByTool(t *testing.T) {
	plan := `{"summary":"План","steps":` + subjectSteps(1) + `}`
	tests := []struct {
		name string
		tool string
		args string
		ok   bool
	}{
		{"plan over the call limit", "show_plan", padded(plan, 9<<10), true},
		{"plan at its limit", "show_plan", padded(plan, MaxPlanArgumentsBytes), true},
		{"plan over its limit", "show_plan", padded(plan, MaxPlanArgumentsBytes+1), false},
		{"other tool at the call limit", "list_subjects", padded(`{}`, MaxArgumentsBytes), true},
		{"other tool over the call limit", "list_subjects", padded(`{}`, MaxArgumentsBytes+1), false},
		{"reply is not a plan", "reply", padded(`{"text":"Готово"}`, MaxArgumentsBytes+1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCall("c1", tt.tool, tt.args)
			if tt.ok != (err == nil) || err != nil && !errors.Is(err, ErrInvalidArguments) {
				t.Fatalf("ParseCall(%d bytes) = %v", len(tt.args), err)
			}
		})
	}
}
