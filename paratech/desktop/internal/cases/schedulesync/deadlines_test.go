package schedulesync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// fakeRemovals are the deadline event removals the recording deletion left (TASK-095).
type fakeRemovals struct {
	mu  sync.Mutex
	ids []shared.AssignmentID
}

func (f *fakeRemovals) ListDeadlineRemovals(_ context.Context, limit int) ([]shared.AssignmentID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids[:min(limit, len(f.ids))]), nil
}

func (f *fakeRemovals) PurgeDeadlineRemoval(_ context.Context, id shared.AssignmentID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = slices.DeleteFunc(f.ids, func(x shared.AssignmentID) bool { return x == id })
	return nil
}

func (f *fakeRemovals) left() []shared.AssignmentID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ids)
}

// fakeDeadlines is the deadline side of the calendar: the deletes in order and the errors per assignment.
type fakeDeadlines struct {
	mu      sync.Mutex
	t       *testing.T
	deleted []string // "<calendar>/<assignment>"
	fail    map[shared.AssignmentID]error
}

func (f *fakeDeadlines) DeleteDeadline(ctx context.Context, calendarID string, id shared.AssignmentID) error {
	if ctx.Value(txKey{}) != nil {
		f.t.Error("deadline event deleted inside Tx.Do")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[id]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, calendarID+"/"+string(id))
	return nil
}

func (f *fakeDeadlines) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

var _ DeadlineRemovals = (*fakeRemovals)(nil)

func withDeadlines(t *testing.T, e *syncEnv, ids ...shared.AssignmentID) (*fakeRemovals, *fakeDeadlines) {
	t.Helper()
	r, d := &fakeRemovals{ids: ids}, &fakeDeadlines{t: t, fail: map[shared.AssignmentID]error{}}
	var err error
	e.sync, err = NewCalendarSync(CalendarDeps{
		Lessons: e.store, Calendar: e.cal, Account: e.account, Jobs: e.jobs, Clock: e.clock, IDs: e.ids, Tx: e.store,
		Removals: r, Deadlines: d,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, d
}

func TestSyncRemovesDeadlineEventsOfDeletedAssignments(t *testing.T) {
	e := newSyncEnv(t)
	e.seed(t, "l1", content("Lesson", at(0)))
	removals, deadlines := withDeadlines(t, e, "a1", "a2")

	e.mustSync(t)
	if got := deadlines.calls(); !slices.Equal(got, []string{"cal-1/a1", "cal-1/a2"}) {
		t.Fatalf("deadline deletes = %v", got)
	}
	if left := removals.left(); len(left) != 0 {
		t.Fatalf("removals left = %v", left)
	}
	if _, ok := e.cal.current()["l1"]; !ok {
		t.Fatal("the lessons were not synced in the same run")
	}

	e.mustSync(t)
	if got := deadlines.calls(); len(got) != 2 {
		t.Fatalf("a second run deleted again: %v", got)
	}
}

func TestSyncKeepsADeadlineRemovalThatFails(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		jobFail bool // the run ends with an error and the job is retried
	}{
		{"only this event", fmt.Errorf("%w: %w", ErrLessonRejected, backoff.Permanent(errors.New("400"))), false},
		{"transient", backoff.Transient(errors.New("503")), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSyncEnv(t)
			removals, deadlines := withDeadlines(t, e, "a-bad", "a-ok")
			deadlines.fail["a-bad"] = tc.err

			err := e.handle(t)
			if (err != nil) != tc.jobFail {
				t.Fatalf("Handle = %v, want an error: %v", err, tc.jobFail)
			}
			left := removals.left()
			if !slices.Contains(left, "a-bad") {
				t.Fatalf("the failed removal was purged: left %v", left)
			}
			if !tc.jobFail && (slices.Contains(left, "a-ok") || !slices.Equal(deadlines.calls(), []string{"cal-1/a-ok"})) {
				t.Fatalf("the other removal was not carried out: left %v, deletes %v", left, deadlines.calls())
			}

			delete(deadlines.fail, "a-bad")
			e.mustSync(t)
			if left := removals.left(); len(left) != 0 {
				t.Fatalf("removals left after the retry = %v", left)
			}
		})
	}
}

func TestNewCalendarSyncNeedsBothDeadlinePorts(t *testing.T) {
	e := newSyncEnv(t)
	d := CalendarDeps{
		Lessons: e.store, Calendar: e.cal, Account: e.account, Jobs: e.jobs, Clock: e.clock, IDs: e.ids, Tx: e.store,
		Removals: &fakeRemovals{},
	}
	if _, err := NewCalendarSync(d); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewCalendarSync with removals only = %v, want ErrInvalidConfig", err)
	}
}
