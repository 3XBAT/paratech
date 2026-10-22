package thread

import (
	"errors"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// A call the Driver ran before its context ended keeps its real result; only the others are «not done».
func TestInterruptKeepsResultsOfCallsThatRan(t *testing.T) {
	th := newThread(t)
	stepWith(t, th, "c1", "c2")
	if err := th.Answer(ok("c1"), values.Ref{}, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Interrupt(at(5)); err != nil {
		t.Fatal(err)
	}
	res := takeAll(t, th)
	if res[0] != ok("c1") || res[1] != values.RefusalResult("c2", values.RefuseNotDone) {
		t.Fatalf("results = %+v", res)
	}
	turn, _ := th.Turn()
	if turn.Status() != StatusStopped || turn.StopReason() != values.StopOffline {
		t.Fatalf("request = %s %s", turn.Status(), turn.StopReason())
	}
	if err := th.Continue(at(6)); err != nil {
		t.Fatalf("Continue after Interrupt = %v", err)
	}
}

func TestInterruptKeepsTheStudentsStop(t *testing.T) {
	th := newThread(t)
	stepWith(t, th, "c1")
	if err := th.Stop(values.StopUser, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Interrupt(at(5)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.StopReason() != values.StopUser {
		t.Fatalf("stop reason = %s", turn.StopReason())
	}
	if err := (&Thread{}).Interrupt(at(5)); !errors.Is(err, ErrNoTurn) {
		t.Fatalf("Interrupt without a request = %v", err)
	}
}

// Replacing a ready result runs only as a step of an approved plan.
func TestReplacingReadyResultRunsInAPlan(t *testing.T) {
	th := newThread(t)
	f := Facts{Context: context(t), CostMicroUSD: 1, ReplacesReady: true}
	if _, err := th.Admit(call(t, "make_summary", `{"recording":"r1"}`), f, at(2)); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("without a plan = %v", err)
	}
	if err := th.ProposePlan(planSteps(t, `[{"tool":"make_summary","arguments":{"recording":"r1"}}]`), context(t), at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(3)); err != nil {
		t.Fatal(err)
	}
	adm, err := th.Admit(call(t, "make_summary", `{"recording":"r1"}`), f, at(4))
	if err != nil || adm.PlanStep != 0 || !adm.Paid {
		t.Fatalf("in the plan = %+v, %v", adm, err)
	}
	if turn, _ := th.Turn(); turn.Budget().PaidUnplanned != 0 {
		t.Fatalf("a planned paid job counted as unplanned: %+v", turn.Budget())
	}
}
