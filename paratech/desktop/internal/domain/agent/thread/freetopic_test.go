package thread

import (
	"testing"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

const topicArgs = `{"topic":"Реформы Петра I, презентация"}`

// TestFreeTopicAdmission (TASK-188, TASK-342): a work on a free topic outside a plan runs at once as the one paid job
// without a plan (no notice card waits); a second write still needs a plan; as a step of an approved plan it runs as
// any step.
func TestFreeTopicAdmission(t *testing.T) {
	facts := Facts{Context: context(t)}
	topicPlan := `[{"tool":"triage_assignment","arguments":` + topicArgs + `}]`
	subjectPlan := `[{"tool":"create_subject","arguments":{"name":"Химия"}}]`

	type admit struct {
		id, tool, args string
		class          actionpolicy.Class // "" when refused
		refusal        values.Refusal
	}
	tests := []struct {
		name       string
		plan       string // "" none
		calls      []admit
		wantWrites int
		wantPaid   int
	}{
		{"the paid job without a plan", "", []admit{
			{"c1", "triage_assignment", topicArgs, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, "", values.RefusePlanRequired},
		}, 1, 1},
		{"an assignment still needs a plan", "", []admit{
			{"c1", "triage_assignment", `{"assignment":"a1"}`, "", values.RefusePlanRequired},
		}, 0, 0},
		{"after another write: a plan", "", []admit{
			{"c1", "create_subject", `{"name":"Химия"}`, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, "", values.RefusePlanRequired},
		}, 1, 0},
		{"a step of the approved plan", topicPlan, []admit{
			{"c1", "triage_assignment", topicArgs, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, "", values.RefuseNotInPlan},
		}, 0, 0}, // a plan step is not a write of the budget (ADR-0033 §4)
		{"outside the approved plan: not in plan", subjectPlan, []admit{
			{"c1", "triage_assignment", topicArgs, "", values.RefuseNotInPlan},
		}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			if err := th.Observe(ref(t, values.KindAssignment, "a1")); err != nil {
				t.Fatal(err)
			}
			if tt.plan != "" {
				if err := th.ProposePlan(planSteps(t, tt.plan), context(t), at(2)); err != nil {
					t.Fatal(err)
				}
				if err := th.ApprovePlan(at(2)); err != nil {
					t.Fatal(err)
				}
			}
			for _, c := range tt.calls {
				adm, err := th.Admit(callID(t, c.id, c.tool, c.args), facts, at(3))
				if c.class == "" {
					if why, _ := RefusalOf(err); why != c.refusal {
						t.Fatalf("%s: Admit = %+v, %v; want %s", c.id, adm, err, c.refusal)
					}
					continue
				}
				if err != nil || adm.Class != c.class || (c.tool == "triage_assignment" && !adm.Paid) {
					t.Fatalf("%s: Admit = %+v, %v; want class %s", c.id, adm, err, c.class)
				}
			}
			turn, _ := th.Turn()
			if b := turn.Budget(); b.Writes != tt.wantWrites || b.PaidUnplanned != tt.wantPaid {
				t.Fatalf("budget = %+v, want writes %d, paid %d", b, tt.wantWrites, tt.wantPaid)
			}
		})
	}
}

// The free topic work runs: the request moves on to EXECUTING, as for any write.
func TestFreeTopicExecutes(t *testing.T) {
	th := newThread(t)
	adm, err := th.Admit(callID(t, "c1", "triage_assignment", topicArgs), Facts{}, at(3))
	if err != nil || adm != (Admission{Class: actionpolicy.ClassU, Paid: true, PlanStep: -1}) {
		t.Fatalf("Admit = %+v, %v", adm, err)
	}
	if s := status(t, th); s != StatusExecuting {
		t.Fatalf("status %s, want EXECUTING", s)
	}
}
