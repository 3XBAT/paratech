package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

// call10 parses a call of catalog v10.
func call10(t *testing.T, id, tool, args string) values.Call {
	t.Helper()
	c, err := values.ParseCallAt(id, tool, args, values.CatalogWave10)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plan10(t *testing.T, steps string) []values.PlanStep {
	t.Helper()
	return call10(t, "p1", "show_plan", `{"summary":"План","steps":`+steps+`}`).Args().PlanSteps("steps")
}

// assignmentContext is context with assignment a1.
func assignmentContext(t *testing.T) values.RefSet {
	ctx := context(t)
	ctx.Add(ref(t, values.KindAssignment, "a1"))
	return ctx
}

const deleteA1 = `{"assignment":"a1","action":"delete"}`

// TestAdmitDecidesPerCall: the class of change_assignment{delete} follows the Google event fact (О-6 б), outside and
// inside an approved plan; other tools keep their class and the card and NoUndo of the decision are carried.
func TestAdmitDecidesPerCall(t *testing.T) {
	ctx := assignmentContext(t)
	tests := []struct {
		name  string
		tool  string
		args  string
		event bool
		want  Admission
	}{
		{"delete without an event is U", "change_assignment", deleteA1, false,
			Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"delete with an event is C with its card", "change_assignment", deleteA1, true,
			Admission{Class: actionpolicy.ClassC, PlanStep: -1, Card: values.CardDeleteAssignment}},
		{"edit with an event stays U (C-4)", "change_assignment", `{"assignment":"a1","action":"edit","title":"Эссе"}`, true,
			Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"restore is U", "change_assignment", `{"assignment":"a1","action":"restore"}`, true,
			Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"mark done is U", "mark_assignments_done", `{"assignments":["a1"],"done":true}`, false,
			Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"stop recording carries NoUndo", "recording", `{"action":"stop"}`, false,
			Admission{Class: actionpolicy.ClassU, PlanStep: -1, NoUndo: true}},
		{"list is R", "list_assignments", `{"state":"all"}`, true, Admission{Class: actionpolicy.ClassR, PlanStep: -1}},
		{"create_subject keeps U", "create_subject", `{"name":"Физика"}`, true, Admission{Class: actionpolicy.ClassU, PlanStep: -1}},
		{"propose_delete_recording keeps C", "propose_delete_recording", `{"recording":"r1"}`, false,
			Admission{Class: actionpolicy.ClassC, PlanStep: -1, Card: values.CardConfirm}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			got, err := th.Admit(call10(t, "c1", tt.tool, tt.args), Facts{Context: ctx, HasCalendarEvent: tt.event}, at(2))
			if err != nil || got != tt.want {
				t.Fatalf("Admit = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

// TestAdmitDeleteInAPlan: a delete step is a U step of the plan; when the event appeared before it runs, the call is
// C (its card, no step used, no write), and a delete without one still matches the step.
func TestAdmitDeleteInAPlan(t *testing.T) {
	ctx := assignmentContext(t)
	th := newThread(t)
	steps := plan10(t, `[{"tool":"change_assignment","arguments":`+deleteA1+`}]`)
	if err := th.ProposePlan(steps, ctx, at(2)); err != nil {
		t.Fatalf("ProposePlan with a delete step: %v", err)
	}
	if err := th.ApprovePlan(at(3)); err != nil {
		t.Fatal(err)
	}

	adm, err := th.Admit(call10(t, "c1", "change_assignment", deleteA1), Facts{Context: ctx, HasCalendarEvent: true}, at(4))
	if err != nil || adm.Class != actionpolicy.ClassC || adm.Card != values.CardDeleteAssignment || adm.PlanStep != -1 {
		t.Fatalf("delete with an event in a plan = %+v, %v; want C with its card, no step", adm, err)
	}
	adm, err = th.Admit(call10(t, "c2", "change_assignment", deleteA1), Facts{Context: ctx}, at(5))
	if err != nil || adm.Class != actionpolicy.ClassU || adm.PlanStep != 0 {
		t.Fatalf("delete without an event in a plan = %+v, %v; want U on step 0", adm, err)
	}
	if _, err := th.Admit(call10(t, "c3", "change_assignment", deleteA1), Facts{Context: ctx}, at(6)); !errors.Is(err, ErrNotInPlan) {
		t.Fatalf("a second delete = %v, want ErrNotInPlan (the step is used)", err)
	}
}

// TestAdmitDeleteWithAnEventIsNoWrite: the C card does not count as a write, so a U call after it still runs at once.
func TestAdmitDeleteWithAnEventIsNoWrite(t *testing.T) {
	ctx := assignmentContext(t)
	th := newThread(t)
	if _, err := th.Admit(call10(t, "c1", "change_assignment", deleteA1), Facts{Context: ctx, HasCalendarEvent: true}, at(2)); err != nil {
		t.Fatal(err)
	}
	adm, err := th.Admit(call10(t, "c2", "create_subject", `{"name":"Физика"}`), Facts{Context: ctx}, at(3))
	if err != nil || adm.Class != actionpolicy.ClassU {
		t.Fatalf("first write after the card = %+v, %v; want U at once", adm, err)
	}
}

func TestProposePlanDecidesSteps(t *testing.T) {
	ctx := assignmentContext(t)
	tests := []struct {
		name  string
		steps string
		ok    bool
	}{
		{"delete, edit, restore, mark done", `[{"tool":"change_assignment","arguments":` + deleteA1 + `},` +
			`{"tool":"change_assignment","arguments":{"assignment":"a1","action":"edit","due":"2026-11-10"}},` +
			`{"tool":"change_assignment","arguments":{"assignment":"a1","action":"restore"}},` +
			`{"tool":"mark_assignments_done","arguments":{"assignments":["a1"],"done":true}}]`, true},
		{"a read step", `[{"tool":"list_assignments","arguments":{"state":"done"}}]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newThread(t).ProposePlan(plan10(t, tt.steps), ctx, at(2))
			if tt.ok != (err == nil) || (!tt.ok && !errors.Is(err, ErrInvalidPlan)) {
				t.Fatalf("ProposePlan = %v, ok %v", err, tt.ok)
			}
		})
	}
}

func TestUnknownActionIsARefusal(t *testing.T) {
	why, ok := RefusalOf(actionpolicy.ErrUnknownAction)
	if !ok || why != values.RefuseInvalidArgs {
		t.Fatalf("RefusalOf(ErrUnknownAction) = %q, %v; want invalid_arguments", why, ok)
	}
}
