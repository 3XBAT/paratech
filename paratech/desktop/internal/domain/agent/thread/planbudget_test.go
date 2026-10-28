package thread

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/agent/values"
)

// subjectsPlan returns a plan of n create_subject steps named planSubject(0..n-1).
func subjectsPlan(n int) string {
	s := `[`
	for i := range n {
		if i > 0 {
			s += `,`
		}
		s += `{"tool":"create_subject","arguments":{"name":"` + planSubject(i) + `"}}`
	}
	return s + `]`
}

func planSubject(i int) string { return fmt.Sprintf("Предмет %d", i) }

// approved returns a thread whose request has an approved plan of n create_subject steps.
func approved(t *testing.T, n int) *Thread {
	t.Helper()
	th := newThread(t)
	if err := th.ProposePlan(planSteps(t, subjectsPlan(n)), nil, at(2)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(3)); err != nil {
		t.Fatal(err)
	}
	return th
}

// runStep runs one planner step with one call and the given input tokens.
func runStep(t *testing.T, th *Thread, tokens int) error {
	t.Helper()
	if _, err := th.BeginStep(at(4)); err != nil {
		return err
	}
	return th.EndStep(1, tokens, at(4))
}

// ADR-0033 §4: the one «Да» covers every step of the plan; its steps are not writes of the budget.
func TestLongPlanRunsWithoutWriteLimit(t *testing.T) {
	const n = 30
	th := approved(t, n)
	for i := range n {
		if err := runStep(t, th, 13_300); err != nil {
			t.Fatalf("step %d: %v", i+1, err)
		}
		adm, err := th.Admit(callID(t, fmt.Sprintf("c%d", i), "create_subject", `{"name":"`+planSubject(i)+`"}`), Facts{}, at(5))
		if err != nil || adm.PlanStep != i {
			t.Fatalf("write %d: %+v, %v", i+1, adm, err)
		}
	}
	turn, _ := th.Turn()
	if turn.Status() != StatusExecuting || turn.Budget().Writes != 0 || turn.Budget().Steps != n {
		t.Fatalf("after the plan: %s %+v", turn.Status(), turn.Budget())
	}

	// The closing reply is one more step under the plan limits, though every step is used.
	if err := runStep(t, th, 13_300); err != nil {
		t.Fatalf("closing step: %v", err)
	}

	// A write outside the plan is refused, not counted and does not stop the request.
	_, err := th.Admit(call(t, "create_subject", `{"name":"Ещё"}`), Facts{}, at(6))
	if !errors.Is(err, ErrNotInPlan) {
		t.Fatalf("write outside the plan = %v", err)
	}
	if turn, _ := th.Turn(); turn.Status() != StatusExecuting || turn.Budget().Writes != 0 {
		t.Fatalf("after a refused write: %s %+v", turn.Status(), turn.Budget())
	}
}

func TestPlanSize(t *testing.T) {
	steps := planSteps(t, subjectsPlan(values.MaxPlanSteps))
	th := newThread(t)
	if err := th.ProposePlan(steps, nil, at(2)); err != nil {
		t.Fatalf("plan of %d steps = %v", values.MaxPlanSteps, err)
	}
	if err := (values.PlanCard{Summary: "План", Steps: steps}).Validate(); err != nil {
		t.Fatalf("card of %d steps = %v", values.MaxPlanSteps, err)
	}

	over := slices.Concat(steps, planSteps(t, subjectsPlan(1)))
	th = newThread(t)
	if err := th.ProposePlan(over, nil, at(2)); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("plan of %d steps = %v", len(over), err)
	}
	if status(t, th) != StatusPlanning {
		t.Fatal("refused plan changed the status")
	}
	if err := (values.PlanCard{Summary: "План", Steps: over}).Validate(); !errors.Is(err, values.ErrInvalidCard) {
		t.Fatalf("card of %d steps = %v", len(over), err)
	}
}

func TestStepLimitsByMode(t *testing.T) {
	t.Run("steps under a plan", func(t *testing.T) {
		th := approved(t, 1)
		for i := range MaxPlanRunSteps {
			if err := runStep(t, th, 10); err != nil {
				t.Fatalf("step %d: %v", i+1, err)
			}
		}
		_, err := th.BeginStep(at(20))
		assertBudgetStop(t, th, err)
	})
	t.Run("input tokens under a plan", func(t *testing.T) {
		th := approved(t, 1)
		if err := runStep(t, th, MaxPlanInputTokens-1); err != nil {
			t.Fatal(err)
		}
		if err := runStep(t, th, 1); err != nil {
			t.Fatalf("step under the plan token limit: %v", err)
		}
		_, err := th.BeginStep(at(20))
		assertBudgetStop(t, th, err)
	})
	t.Run("input tokens without a plan", func(t *testing.T) {
		th := newThread(t)
		if err := runStep(t, th, MaxInputTokens-1); err != nil {
			t.Fatal(err)
		}
		if err := runStep(t, th, 1); err != nil {
			t.Fatalf("step under the token limit: %v", err)
		}
		_, err := th.BeginStep(at(20))
		assertBudgetStop(t, th, err)
	})
}

// «Изменить» drops the approved plan: the request is back to the limits without a plan, and a write that ran under
// the plan makes the next unplanned write the second one.
func TestRevisedPlanLimits(t *testing.T) {
	th := approved(t, 2)
	for i := range MaxSteps {
		if err := runStep(t, th, 10); err != nil {
			t.Fatalf("step %d: %v", i+1, err)
		}
	}
	if _, err := th.Admit(call(t, "create_subject", `{"name":"`+planSubject(0)+`"}`), Facts{}, at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.ProposePlan(planSteps(t, subjectsPlan(1)), nil, at(6)); err != nil {
		t.Fatal(err)
	}
	if err := th.RevisePlan(at(7)); err != nil {
		t.Fatal(err)
	}
	turn, _ := th.Turn()
	if turn.Budget().Writes != 1 || len(turn.ApprovedPlan()) != 0 {
		t.Fatalf("after «Изменить»: %+v, approved %d", turn.Budget(), len(turn.ApprovedPlan()))
	}
	if _, err := th.Admit(call(t, "create_subject", `{"name":"Химия"}`), Facts{}, at(8)); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("unplanned write after a plan ran = %v", err)
	}
	_, err := th.BeginStep(at(9))
	assertBudgetStop(t, th, err)
}

// A second «Да» resets the used steps of the first plan; a step that ran under it still counts after «Изменить».
func TestRevisedPlanAfterSecondApproval(t *testing.T) {
	th := approved(t, 1)
	if err := runStep(t, th, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Admit(call(t, "create_subject", `{"name":"`+planSubject(0)+`"}`), Facts{}, at(5)); err != nil {
		t.Fatal(err)
	}
	if err := th.ProposePlan(planSteps(t, subjectsPlan(1)), nil, at(6)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(7)); err != nil {
		t.Fatal(err)
	}
	if err := th.ProposePlan(planSteps(t, subjectsPlan(1)), nil, at(8)); err != nil {
		t.Fatal(err)
	}
	if err := th.RevisePlan(at(9)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Budget().Writes != 1 {
		t.Fatalf("after «Изменить»: %+v", turn.Budget())
	}
	if _, err := th.Admit(call(t, "create_subject", `{"name":"Химия"}`), Facts{}, at(10)); !errors.Is(err, ErrPlanRequired) {
		t.Fatalf("unplanned write after a plan ran = %v", err)
	}
}

func TestApprovePlanWithoutWrites(t *testing.T) {
	th := approved(t, 1)
	if err := th.ProposePlan(planSteps(t, subjectsPlan(1)), nil, at(6)); err != nil {
		t.Fatal(err)
	}
	if err := th.ApprovePlan(at(7)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Budget() != (Budget{}) {
		t.Fatalf("budget = %+v", turn.Budget())
	}
}

func TestRevisedPlanWithoutWrites(t *testing.T) {
	th := approved(t, 1)
	if err := th.ProposePlan(planSteps(t, subjectsPlan(1)), nil, at(6)); err != nil {
		t.Fatal(err)
	}
	if err := th.RevisePlan(at(7)); err != nil {
		t.Fatal(err)
	}
	if turn, _ := th.Turn(); turn.Budget() != (Budget{}) {
		t.Fatalf("budget = %+v", turn.Budget())
	}
}

func TestRestoreBudgetModes(t *testing.T) {
	restore := func(t *testing.T, th *Thread, b Budget) error {
		t.Helper()
		s := th.State()
		s.Version = 1
		s.Current.Budget = b
		_, err := Restore(s)
		return err
	}
	if err := restore(t, newThread(t), Budget{Steps: MaxSteps, Writes: MaxWrites, InputTokens: MaxInputTokens}); err != nil {
		t.Fatalf("request without a plan: %v", err)
	}
	if err := restore(t, approved(t, 3), Budget{Steps: MaxPlanRunSteps, InputTokens: MaxPlanInputTokens}); err != nil {
		t.Fatalf("request under a plan: %v", err)
	}
	if err := restore(t, approved(t, 3), Budget{Steps: MaxPlanRunSteps + 1}); !errors.Is(err, ErrInvalidThread) {
		t.Fatalf("steps over the plan limit = %v", err)
	}
}
