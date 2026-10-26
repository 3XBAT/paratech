package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

const topicArgs = `{"topic":"Реформы Петра I, презентация"}`

// TestFreeTopicAdmission (TASK-188): a work on a free topic outside a plan waits on the free-work card while the
// student wants it (class UI: nothing runs, nothing counted); without the card it is the one paid job without a plan;
// a second write still needs a plan; as a step of an approved plan it runs as any step.
func TestFreeTopicAdmission(t *testing.T) {
	notice := Facts{Context: context(t), FreeWorkNotice: true}
	noNotice := Facts{Context: context(t)}
	topicPlan := `[{"tool":"triage_assignment","arguments":` + topicArgs + `}]`
	subjectPlan := `[{"tool":"create_subject","arguments":{"name":"Химия"}}]`

	type admit struct {
		id, tool, args string
		facts          Facts
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
		{"card while the notice is on", "", []admit{
			{"c1", "triage_assignment", topicArgs, notice, actionpolicy.ClassUI, ""},
			{"c2", "triage_assignment", topicArgs, notice, actionpolicy.ClassUI, ""},
		}, 0, 0},
		{"the notice changes nothing for an assignment", "", []admit{
			{"c1", "triage_assignment", `{"assignment":"a1"}`, notice, "", values.RefusePlanRequired},
		}, 0, 0},
		{"without the card: the paid job without a plan", "", []admit{
			{"c1", "triage_assignment", topicArgs, noNotice, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, noNotice, "", values.RefusePlanRequired},
		}, 1, 1},
		{"without the card after another write: a plan", "", []admit{
			{"c1", "create_subject", `{"name":"Химия"}`, noNotice, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, noNotice, "", values.RefusePlanRequired},
		}, 1, 0},
		{"a step of the approved plan runs without the card", topicPlan, []admit{
			{"c1", "triage_assignment", topicArgs, notice, actionpolicy.ClassU, ""},
			{"c2", "triage_assignment", topicArgs, notice, "", values.RefuseNotInPlan},
		}, 1, 0},
		{"outside the approved plan: not in plan", subjectPlan, []admit{
			{"c1", "triage_assignment", topicArgs, notice, "", values.RefuseNotInPlan},
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
				adm, err := th.Admit(callID(t, c.id, c.tool, c.args), c.facts, at(3))
				if c.class == "" {
					if why, _ := RefusalOf(err); why != c.refusal {
						t.Fatalf("%s: Admit = %+v, %v; want %s", c.id, adm, err, c.refusal)
					}
					continue
				}
				if err != nil || adm.Class != c.class {
					t.Fatalf("%s: Admit = %+v, %v; want class %s", c.id, adm, err, c.class)
				}
				if c.class == actionpolicy.ClassUI && (adm.Paid || adm.PlanStep != -1) {
					t.Fatalf("%s: a card admission = %+v", c.id, adm)
				}
			}
			turn, _ := th.Turn()
			if b := turn.Budget(); b.Writes != tt.wantWrites || b.PaidUnplanned != tt.wantPaid {
				t.Fatalf("budget = %+v, want writes %d, paid %d", b, tt.wantWrites, tt.wantPaid)
			}
		})
	}
}

// The card admission keeps the request planning: the planner goes on and answers the student.
func TestFreeTopicCardKeepsPlanning(t *testing.T) {
	th := newThread(t)
	if _, err := th.Admit(callID(t, "c1", "triage_assignment", topicArgs), Facts{FreeWorkNotice: true}, at(3)); err != nil {
		t.Fatal(err)
	}
	if s := status(t, th); s != StatusPlanning {
		t.Fatalf("status %s, want PLANNING", s)
	}
}

func TestFreeWorkCall(t *testing.T) {
	waiting, err := values.FreeWorkResult("c1")
	if err != nil {
		t.Fatal(err)
	}
	topic := values.RawCall{CallID: "c1", Tool: "triage_assignment", ArgumentsJSON: topicArgs}
	calls := func(turn int, raws ...values.RawCall) values.HistoryEntry {
		return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Calls: &values.SealedCalls{Calls: raws, Seal: []byte("s")}}}
	}
	results := func(turn int, rs ...values.ToolResult) values.HistoryEntry {
		return values.HistoryEntry{Turn: turn, Entry: values.HistoryTurn{Results: rs}}
	}
	student := values.HistoryEntry{Turn: 2, Entry: values.HistoryTurn{StudentText: "сделай презентацию"}}

	tests := []struct {
		name    string
		entries []values.HistoryEntry
		turn    int
		callID  string
		ok      bool
	}{
		{"the waiting call", []values.HistoryEntry{student, calls(2, topic), results(2, waiting)}, 2, "c1", true},
		{"another request", []values.HistoryEntry{student, calls(2, topic), results(2, waiting)}, 1, "c1", false},
		{"a made-up call", []values.HistoryEntry{student, calls(2, topic), results(2, waiting)}, 2, "c9", false},
		{"no history (another thread)", nil, 2, "c1", false},
		{"not answered yet", []values.HistoryEntry{calls(2, topic)}, 2, "c1", false},
		{"refused", []values.HistoryEntry{calls(2, topic), results(2, values.RefusalResult("c1", values.RefusePlanRequired))}, 2, "c1", false},
		{"ran", []values.HistoryEntry{calls(2, topic), results(2, values.ToolResult{CallID: "c1", ResultJSON: `{"work":"w1"}`})}, 2, "c1", false},
		{"an assignment, not a topic", []values.HistoryEntry{
			calls(2, values.RawCall{CallID: "c1", Tool: "triage_assignment", ArgumentsJSON: `{"assignment":"a1"}`}), results(2, waiting),
		}, 2, "c1", false},
		{"another tool", []values.HistoryEntry{
			calls(2, values.RawCall{CallID: "c1", Tool: "search", ArgumentsJSON: `{"query":"x"}`}), results(2, waiting),
		}, 2, "c1", false},
		{"arguments that no longer parse", []values.HistoryEntry{
			calls(2, values.RawCall{CallID: "c1", Tool: "triage_assignment", ArgumentsJSON: `{"topic":"x","assignment":"a1"}`}), results(2, waiting),
		}, 2, "c1", false},
		{"the same call id twice in the request", []values.HistoryEntry{
			calls(2, topic), results(2, waiting), calls(2, topic), results(2, waiting),
		}, 2, "c1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, adm, err := FreeWorkCall(tt.entries, tt.turn, tt.callID)
			if !tt.ok {
				if !errors.Is(err, ErrUnknownCall) {
					t.Fatalf("FreeWorkCall = %+v, %v; want ErrUnknownCall", c, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q, _ := c.Args().Query("topic"); c.ID() != "c1" || string(q) != "Реформы Петра I, презентация" {
				t.Fatalf("call = %+v", c)
			}
			if want := (Admission{Class: actionpolicy.ClassU, Paid: true, PlanStep: -1}); adm != want {
				t.Fatalf("admission = %+v, want %+v", adm, want)
			}
		})
	}
}
