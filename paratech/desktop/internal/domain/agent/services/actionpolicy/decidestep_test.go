package actionpolicy

import (
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

func TestDecideStep(t *testing.T) {
	plan := call10(t, "show_plan", `{"summary":"План","steps":[`+
		`{"tool":"change_assignment","arguments":{"assignment":"a1","action":"delete"}},`+
		`{"tool":"recording","arguments":{"action":"stop"}},`+
		`{"tool":"create_subject","arguments":{"name":"Физика"}}]}`)
	steps := plan.Args().PlanSteps("steps")
	tests := []struct {
		name string
		step values.PlanStep
		f    Facts
		want Decision
	}{
		{"delete, no facts yet", steps[0], Facts{}, Decision{Rule: Rule{Class: ClassU}}},
		{"delete with an event", steps[0], Facts{HasCalendarEvent: true},
			Decision{Rule: Rule{Class: ClassC}, Card: values.CardDeleteAssignment}},
		{"stop recording", steps[1], Facts{}, Decision{Rule: Rule{Class: ClassU}, NoUndo: true}},
		{"a tool without actions", steps[2], Facts{}, Decision{Rule: Rule{Class: ClassU}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecideStep(tt.step, tt.f)
			if err != nil || got != tt.want {
				t.Fatalf("DecideStep = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}
