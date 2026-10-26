package thread

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

// TASK-170: paid work the student did not approve (triage_assignment; generate_draft until catalog v4). These tests pin
// the rule of ADR-0021 «Plan first» and «Budget per request» against every split of calls the planner can choose.

func draftRaw(id string, n int) values.RawCall {
	return values.RawCall{CallID: id, Tool: "triage_assignment", ArgumentsJSON: fmt.Sprintf(`{"assignment":"a%d"}`, n)}
}

// observeAssignments makes a1..an known, as list_assignments results would.
func observeAssignments(t *testing.T, th *Thread, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if err := th.Observe(ref(t, values.KindAssignment, fmt.Sprintf("a%d", i))); err != nil {
			t.Fatal(err)
		}
	}
}

// rehydrate stores and restores the thread, as every Submit of the case does between steps.
func rehydrate(t *testing.T, th *Thread) *Thread {
	t.Helper()
	s := th.State()
	s.Version = max(s.Version, 1)
	back, err := Restore(s)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

// decideStep runs one planner step the way cases/agentturn does: the step ends with the calls, each call is admitted
// or refused in order, an admitted call runs (failed tells it ran without effect) and the results are taken. It
// returns the assignments of the calls that ran and the refusal of every other call.
func decideStep(t *testing.T, th *Thread, f Facts, failed bool, calls ...values.RawCall) ([]string, map[string]values.Refusal) {
	t.Helper()
	now := next(th)
	if _, err := th.BeginStep(now); err != nil {
		t.Fatal(err)
	}
	if err := th.EndStep(len(calls), 10, now); err != nil {
		t.Fatal(err)
	}
	if err := th.Expect(calls, now); err != nil {
		t.Fatal(err)
	}
	var ran []string
	refused := map[string]values.Refusal{}
	for _, raw := range calls {
		call, err := values.ParseCall(raw.CallID, raw.Tool, raw.ArgumentsJSON)
		if err != nil {
			t.Fatal(err)
		}
		if turn, _ := th.Turn(); !turn.Accepting() {
			refused[raw.CallID] = values.RefuseNotNow
			if err := th.Answer(values.RefusalResult(raw.CallID, values.RefuseNotNow), values.Ref{}, now); err != nil {
				t.Fatal(err)
			}
			continue
		}
		res := ok(raw.CallID)
		if _, err := th.Admit(call, f, now); err != nil {
			why, isRefusal := RefusalOf(err)
			if !isRefusal {
				t.Fatalf("%s: Admit = %v, not a refusal", raw.CallID, err)
			}
			refused[raw.CallID] = why
			res = values.RefusalResult(raw.CallID, why)
		} else {
			a, _ := call.Args().ID("assignment")
			ran = append(ran, a.ID())
			if failed {
				res = values.ToolResult{CallID: raw.CallID, ResultJSON: `{"ok":false}`, IsError: true}
			}
		}
		if err := th.Answer(res, values.Ref{}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, done := th.TakeResults(now); !done {
		t.Fatal("results are not complete")
	}
	return ran, refused
}

// next is a minute after the last change of the current request.
func next(th *Thread) time.Time {
	turn, _ := th.Turn()
	return turn.UpdatedAt().Add(time.Minute)
}

func TestFiveDraftsWithoutPlan(t *testing.T) {
	ctx := context(t)
	unknown := Facts{Context: ctx} // what input/agent reports today: no cost estimate exists
	cheap := Facts{Context: ctx, CostMicroUSD: 1000}
	tests := []struct {
		name       string
		facts      Facts
		first      []values.RawCall
		second     []values.RawCall
		rehydrate  bool
		failed     bool // the first draft ran without effect (a job that existed, a failure)
		wantRan    []string
		wantPaid   int
		wantWrites int
	}{
		{"unknown cost, 3+2", unknown, []values.RawCall{draftRaw("c1", 1), draftRaw("c2", 2), draftRaw("c3", 3)},
			[]values.RawCall{draftRaw("c4", 4), draftRaw("c5", 5)}, false, false, nil, 0, 0},
		{"unknown cost, 1+4, stored between steps", unknown, []values.RawCall{draftRaw("c1", 1)},
			[]values.RawCall{draftRaw("c2", 2), draftRaw("c3", 3), draftRaw("c4", 4), draftRaw("c5", 5)}, true, false, nil, 0, 0},
		{"cheap, 3+2", cheap, []values.RawCall{draftRaw("c1", 1), draftRaw("c2", 2), draftRaw("c3", 3)},
			[]values.RawCall{draftRaw("c4", 4), draftRaw("c5", 5)}, false, false, []string{"a1"}, 1, 1},
		{"cheap, 1+4", cheap, []values.RawCall{draftRaw("c1", 1)},
			[]values.RawCall{draftRaw("c2", 2), draftRaw("c3", 3), draftRaw("c4", 4), draftRaw("c5", 5)}, false, false, []string{"a1"}, 1, 1},
		{"cheap, stored between steps", cheap, []values.RawCall{draftRaw("c1", 1), draftRaw("c2", 2)},
			[]values.RawCall{draftRaw("c3", 3), draftRaw("c4", 4), draftRaw("c5", 5)}, true, false, []string{"a1"}, 1, 1},
		{"cheap, first ran without effect", cheap, []values.RawCall{draftRaw("c1", 1)},
			[]values.RawCall{draftRaw("c2", 2), draftRaw("c3", 3), draftRaw("c4", 4), draftRaw("c5", 5)}, false, true, []string{"a1"}, 1, 1},
		{"cheap, call ids replayed", cheap, []values.RawCall{draftRaw("c1", 1), draftRaw("c2", 2)},
			[]values.RawCall{draftRaw("c1", 3), draftRaw("c2", 4), draftRaw("c3", 5)}, true, false, []string{"a1"}, 1, 1},
		{"at the threshold, full triage", Facts{Context: ctx, CostMicroUSD: actionpolicy.PlanThresholdMicroUSD, Triage: actionpolicy.TriageFull},
			[]values.RawCall{draftRaw("c1", 1), draftRaw("c2", 2), draftRaw("c3", 3)},
			[]values.RawCall{draftRaw("c4", 4), draftRaw("c5", 5)}, false, false, []string{"a1"}, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := newThread(t)
			observeAssignments(t, th, 5)
			ran, refused := decideStep(t, th, tt.facts, tt.failed, tt.first...)
			if tt.rehydrate {
				th = rehydrate(t, th)
			}
			more, refused2 := decideStep(t, th, tt.facts, false, tt.second...)
			ran = append(ran, more...)

			if !slices.Equal(ran, tt.wantRan) {
				t.Fatalf("ran %v, want %v", ran, tt.wantRan)
			}
			if got := len(refused) + len(refused2); got != len(tt.first)+len(tt.second)-len(tt.wantRan) {
				t.Fatalf("%d refusals", got)
			}
			for _, m := range []map[string]values.Refusal{refused, refused2} {
				for id, why := range m {
					if why != values.RefusePlanRequired {
						t.Fatalf("%s refused with %s, want plan_required", id, why)
					}
				}
			}
			turn, _ := th.Turn()
			if b := turn.Budget(); b.PaidUnplanned != tt.wantPaid || b.Writes != tt.wantWrites {
				t.Fatalf("budget = %+v, want paid %d, writes %d", b, tt.wantPaid, tt.wantWrites)
			}
			if !turn.Status().stepping() {
				t.Fatalf("status %s: a refusal must not end the request", turn.Status())
			}
		})
	}
}

// With the facts of today (cost unknown) no request of a thread runs a paid write without a plan, however it starts.
func TestDraftsAcrossRequestsWithoutPlan(t *testing.T) {
	th := newThread(t)
	observeAssignments(t, th, 5)
	f := Facts{Context: context(t)}
	for i := 1; i <= 5; i++ {
		ran, refused := decideStep(t, th, f, false, draftRaw("c1", i))
		if len(ran) != 0 || refused["c1"] != values.RefusePlanRequired {
			t.Fatalf("request %d: ran %v, refused %v", i, ran, refused)
		}
		if turn, _ := th.Turn(); turn.Budget().PaidUnplanned != 0 || turn.Budget().Writes != 0 {
			t.Fatalf("request %d: budget %+v", i, turn.Budget())
		}
		th = rehydrate(t, th)

		// The next request starts in each of the three ways a thread gets a fresh budget.
		var err error
		end := next(th)
		switch i % 3 {
		case 0:
			if err = th.Complete(end); err == nil {
				err = th.StartTurn(end)
			}
		case 1:
			if err = th.Complete(end); err == nil {
				_, err = th.Rerun(false, end)
			}
		case 2:
			if err = th.Stop(values.StopExpired, end); err == nil {
				err = th.Restart(end)
			}
		}
		if err != nil {
			t.Fatalf("request %d: next request: %v", i, err)
		}
	}
}

func TestDraftsOfApprovedPlan(t *testing.T) {
	ctx := context(t)
	f := Facts{Context: ctx}
	plan := func(n int) string {
		s := `[`
		for i := 1; i <= n; i++ {
			if i > 1 {
				s += `,`
			}
			s += fmt.Sprintf(`{"tool":"triage_assignment","arguments":{"assignment":"a%d"}}`, i)
		}
		return s + `]`
	}

	t.Run("paid steps are capped by the write limit", func(t *testing.T) {
		th := newThread(t)
		observeAssignments(t, th, MaxWrites+1)
		if err := th.ProposePlan(planSteps(t, plan(MaxWrites+1)), ctx, at(2)); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("plan of %d drafts = %v", MaxWrites+1, err)
		}
	})

	t.Run("a proposed plan runs nothing", func(t *testing.T) {
		th := newThread(t)
		observeAssignments(t, th, 5)
		if err := th.ProposePlan(planSteps(t, plan(5)), ctx, at(2)); err != nil {
			t.Fatal(err)
		}
		if _, err := th.Admit(callID(t, "c1", "triage_assignment", `{"assignment":"a1"}`), f, at(3)); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("draft while the plan waits for «Да» = %v", err)
		}
		if err := th.RevisePlan(at(4)); err != nil {
			t.Fatal(err)
		}
		ran, refused := decideStep(t, th, f, false, draftRaw("c1", 1))
		if len(ran) != 0 || refused["c1"] != values.RefusePlanRequired {
			t.Fatalf("after «Изменить»: ran %v, refused %v", ran, refused)
		}
	})

	t.Run("each listed draft once", func(t *testing.T) {
		th := newThread(t)
		observeAssignments(t, th, 6)
		if err := th.ProposePlan(planSteps(t, plan(5)), ctx, at(2)); err != nil {
			t.Fatal(err)
		}
		if err := th.ApprovePlan(at(2)); err != nil {
			t.Fatal(err)
		}
		if err := th.ApprovePlan(at(2)); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("second «Да» = %v", err)
		}

		ran, refused := decideStep(t, th, f, false, draftRaw("c1", 1), draftRaw("c2", 2), draftRaw("c3", 3), draftRaw("c4", 1))
		th = rehydrate(t, th)
		// Replayed call IDs, an unlisted assignment and a used step again.
		more, refused2 := decideStep(t, th, f, false, draftRaw("c1", 4), draftRaw("c2", 5), draftRaw("c3", 6), draftRaw("c4", 5))
		ran = append(ran, more...)

		if !slices.Equal(ran, []string{"a1", "a2", "a3", "a4", "a5"}) {
			t.Fatalf("ran %v", ran)
		}
		if refused["c4"] != values.RefuseNotInPlan || refused2["c3"] != values.RefuseNotInPlan ||
			refused2["c4"] != values.RefuseNotInPlan || len(refused)+len(refused2) != 3 {
			t.Fatalf("refused %v, %v", refused, refused2)
		}
		turn, _ := th.Turn()
		if b := turn.Budget(); b.Writes != 5 || b.PaidUnplanned != 0 {
			t.Fatalf("budget = %+v", b)
		}

		// The approval belongs to its request only.
		if err := th.Complete(at(5)); err != nil {
			t.Fatal(err)
		}
		if err := th.StartTurn(at(6)); err != nil {
			t.Fatal(err)
		}
		ran, refused = decideStep(t, th, f, false, draftRaw("c1", 1))
		if len(ran) != 0 || refused["c1"] != values.RefusePlanRequired {
			t.Fatalf("next request: ran %v, refused %v", ran, refused)
		}
	})
}
