package thread

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/agent/values"
)

// Rerun from every status: allowed on a finished or stopped request, refused without a change otherwise.
func TestRerunFromEveryStatus(t *testing.T) {
	tests := []struct {
		from    Status
		wantErr []error // nil: allowed
	}{
		{StatusDone, nil},
		{StatusFailed, nil},
		{StatusCancelled, nil},
		{StatusStopped, nil},
		{StatusPlanning, []error{ErrInvalidTransition, ErrTurnInProgress}},
		{StatusExecuting, []error{ErrInvalidTransition, ErrTurnInProgress}},
		{StatusAwaitingPlan, []error{ErrInvalidTransition}},
		{StatusAwaitingAnswers, []error{ErrInvalidTransition}},
	}
	for _, tt := range tests {
		t.Run(string(tt.from), func(t *testing.T) {
			th := reach(t, tt.from)
			before := th.State()
			res, err := th.Rerun(false, at(9))
			if tt.wantErr != nil {
				for _, want := range tt.wantErr {
					if !errors.Is(err, want) {
						t.Fatalf("Rerun = %v, want %v", err, want)
					}
				}
				if !reflect.DeepEqual(th.State(), before) || res != nil {
					t.Fatal("a refused Rerun changed the thread")
				}
				return
			}
			if err != nil {
				t.Fatalf("Rerun = %v", err)
			}
			turn, _ := th.Turn()
			if turn.Number() != 2 || turn.Status() != StatusPlanning || turn.Budget() != (Budget{}) ||
				turn.Scope() != before.Current.Scope || len(turn.OpenCalls()) != 0 || turn.StepOpen() {
				t.Fatalf("new request = %+v", th.State().Current)
			}
			if th.HistoryFrom() != before.HistoryFrom {
				t.Fatalf("history start = %d, want %d", th.HistoryFrom(), before.HistoryFrom)
			}
			key, err := th.BeginStep(at(10))
			if err != nil || key != "ag:th1:2:1" {
				t.Fatalf("first key of the repeat = %q, %v", key, err)
			}
		})
	}
}

// Live effects block the repeat in any status it is allowed from, and nothing changes.
func TestRerunBlockedByLiveEffects(t *testing.T) {
	for _, from := range []Status{StatusDone, StatusFailed, StatusCancelled, StatusStopped} {
		t.Run(string(from), func(t *testing.T) {
			th := reach(t, from)
			before := th.State()
			if _, err := th.Rerun(true, at(9)); !errors.Is(err, ErrRerunBlocked) {
				t.Fatalf("Rerun over live effects = %v", err)
			}
			if !reflect.DeepEqual(th.State(), before) {
				t.Fatal("a blocked Rerun changed the thread")
			}
		})
	}
	// A running request is busy even with live effects: the status is checked first.
	th := reach(t, StatusPlanning)
	if _, err := th.Rerun(true, at(9)); !errors.Is(err, ErrTurnInProgress) {
		t.Fatalf("Rerun of a running request = %v", err)
	}
}

// A request stopped (offline) with calls waiting is cancelled; its calls are answered not done and handed back for the
// history of the old request; the repeat keeps the scope.
func TestRerunFromStoppedWithOpenCalls(t *testing.T) {
	scope, err := values.NewScope(ref(t, values.KindSubject, "s1"), values.ScreenSubjects, values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := KeyOf(scope)
	if err != nil {
		t.Fatal(err)
	}
	th, err := New("th1", key, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := th.StartRequest(scope, at(1)); err != nil {
		t.Fatal(err)
	}
	stepWith(t, th, "c1", "c2")
	if err := th.Answer(ok("c1"), values.Ref{}, at(4)); err != nil {
		t.Fatal(err)
	}
	if err := th.Stop(values.StopOffline, at(5)); err != nil {
		t.Fatal(err)
	}

	res, err := th.Rerun(false, at(6))
	if err != nil {
		t.Fatalf("Rerun = %v", err)
	}
	if len(res) != 2 || res[0] != ok("c1") || res[1] != values.RefusalResult("c2", values.RefuseNotDone) {
		t.Fatalf("results of the old request = %+v", res)
	}
	turn, _ := th.Turn()
	if turn.Number() != 2 || turn.Status() != StatusPlanning || turn.Scope() != scope || len(turn.OpenCalls()) != 0 {
		t.Fatalf("new request = %+v", th.State().Current)
	}
	if !turn.StartedAt().Equal(at(6)) {
		t.Fatal("the new request did not start at the time of the repeat")
	}
}

func TestRerunChecks(t *testing.T) {
	if _, err := (&Thread{}).Rerun(false, at(1)); !errors.Is(err, ErrNoTurn) {
		t.Fatalf("Rerun without a request = %v", err)
	}
	th := reach(t, StatusDone)
	before := th.State()
	if _, err := th.Rerun(false, time.Time{}); !errors.Is(err, ErrInvalidThread) || !reflect.DeepEqual(th.State(), before) {
		t.Fatalf("Rerun without a time = %v", err)
	}
	// The repeat is the current request: a second Rerun repeats it, not the old one (the case refuses the old turn).
	if _, err := th.Rerun(false, at(9)); err != nil {
		t.Fatal(err)
	}
	if _, err := th.Rerun(false, at(10)); !errors.Is(err, ErrTurnInProgress) {
		t.Fatalf("Rerun of the running repeat = %v", err)
	}
}
