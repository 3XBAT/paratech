package homeworkflow

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// fakeActivity is AssignmentActivity: the assignments with a work under way and their unfinished jobs.
type fakeActivity struct {
	mu      sync.Mutex
	working map[shared.AssignmentID]bool
	jobs    map[shared.AssignmentID][]shared.JobID
	err     error // returned by Working
	listErr error // returned by ActiveJobs
	inTx    []bool
}

func (f *fakeActivity) Working(ctx context.Context, id shared.AssignmentID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inTx = append(f.inTx, ctx.Value(txKey{}) != nil)
	if f.err != nil {
		return false, f.err
	}
	return f.working[id] || len(f.jobs[id]) > 0, nil
}

func (f *fakeActivity) ActiveJobs(_ context.Context, id shared.AssignmentID) ([]shared.JobID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.jobs[id]), nil
}

// end takes job j of assignment id out of the unfinished ones.
func (f *fakeActivity) end(id shared.AssignmentID, j shared.JobID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[id] = slices.DeleteFunc(f.jobs[id], func(x shared.JobID) bool { return x == j })
}

// fakeJobCanceller is the runner's Cancel: a queued job ends at once, a running one (running) only gets its context
// cancelled and stays unfinished until its handler returns. err fails every cancel; ended then says whether the job
// had ended meanwhile.
type fakeJobCanceller struct {
	mu        sync.Mutex
	act       *fakeActivity
	of        shared.AssignmentID
	running   map[shared.JobID]bool
	err       error
	ended     bool
	cancelled []shared.JobID
	check     func(ctx context.Context) // runs on each cancel
}

func (c *fakeJobCanceller) Cancel(ctx context.Context, id shared.JobID) error {
	if c.check != nil {
		c.check(ctx)
	}
	c.mu.Lock()
	c.cancelled = append(c.cancelled, id)
	err, ended, running := c.err, c.ended, c.running[id]
	c.mu.Unlock()
	if err != nil {
		if ended {
			c.act.end(c.of, id)
		}
		return err
	}
	if !running {
		c.act.end(c.of, id)
	}
	return nil
}

func (c *fakeJobCanceller) calls() []shared.JobID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.cancelled)
}

// lockedBuffer is a log sink safe for concurrent writes.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var (
	_ AssignmentActivity = (*fakeActivity)(nil)
	_ JobCanceller       = (*fakeJobCanceller)(nil)
)

// stopEnv is the create env with the activity, the canceller and a log; it holds one assignment the student added.
type stopEnv struct {
	*createEnv
	act    *fakeActivity
	cancel *fakeJobCanceller
	logs   *lockedBuffer
	id     shared.AssignmentID
	ver    int64
}

func newStopEnv(t *testing.T) *stopEnv {
	t.Helper()
	e := newCreateEnv(t, testSubjects)
	v, err := e.svc.CreateAssignment(context.Background(), NewAssignment{Title: "Реферат"})
	if err != nil {
		t.Fatal(err)
	}
	act := &fakeActivity{working: map[shared.AssignmentID]bool{}, jobs: map[shared.AssignmentID][]shared.JobID{}}
	se := &stopEnv{
		createEnv: e, act: act, cancel: &fakeJobCanceller{act: act, of: v.ID, running: map[shared.JobID]bool{}},
		logs: &lockedBuffer{}, id: v.ID, ver: v.Version,
	}
	e.svc.activity, e.svc.jobs = act, se.cancel
	e.svc.log = slog.New(slog.NewTextHandler(se.logs, nil))
	return se
}

func (e *stopEnv) state() assignment.State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.assignments[e.id]
}

// «Удалить» goes through while a work is triaged or written or a draft or writing job is unfinished; after the commit
// every unfinished job naming the assignment is cancelled once (TASK-338).
func TestDeleteStopsWhatIsUnderWay(t *testing.T) {
	cases := []struct {
		name    string
		working bool           // a work in triage or writing
		jobs    []shared.JobID // unfinished jobs naming it
		running []shared.JobID // of them, the ones a handler runs
		left    []shared.JobID // unfinished after the delete (a running step ends when it returns)
		stopped bool
	}{
		{name: "nothing under way"},
		{name: "a queued writing job", jobs: []shared.JobID{"j-write"}, stopped: true},
		{name: "a running writing job", jobs: []shared.JobID{"j-write"}, running: []shared.JobID{"j-write"},
			left: []shared.JobID{"j-write"}, stopped: true},
		{name: "a work in triage, no job", working: true, stopped: true},
		{name: "a work being written and a draft generation", working: true, jobs: []shared.JobID{"j-write", "j-draft"},
			running: []shared.JobID{"j-draft"}, left: []shared.JobID{"j-draft"}, stopped: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newStopEnv(t)
			e.act.working[e.id], e.act.jobs[e.id] = c.working, slices.Clone(c.jobs)
			for _, j := range c.running {
				e.cancel.running[j] = true
			}
			// The cancel runs after the commit: outside the transaction, on the deleted assignment.
			e.cancel.check = func(ctx context.Context) {
				if ctx.Value(txKey{}) != nil {
					t.Error("a job cancelled inside the transaction")
				}
				if e.state().DeletedAt.IsZero() {
					t.Error("a job cancelled before the delete was stored")
				}
			}

			e.clock.Advance(time.Minute)
			d, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver)
			if err != nil {
				t.Fatalf("DeleteAndStop: %v", err)
			}
			if !d.Assignment.Deleted || d.Assignment.Version != e.ver+1 || d.Stopped != c.stopped {
				t.Fatalf("deletion = %+v, want deleted at %d, stopped %v", d, e.ver+1, c.stopped)
			}
			if got := e.cancel.calls(); !slices.Equal(got, c.jobs) {
				t.Fatalf("cancelled %v, want %v once each", got, c.jobs)
			}
			if got, _ := e.act.ActiveJobs(context.Background(), e.id); !slices.Equal(got, c.left) {
				t.Fatalf("unfinished after the delete = %v, want %v", got, c.left)
			}
			if !slices.Equal(e.act.inTx, []bool{true}) {
				t.Fatalf("Working reads (in a transaction) = %v, want one inside the delete's", e.act.inTx)
			}
			if strings.Contains(e.logs.String(), "level=WARN") {
				t.Fatalf("log = %q, want no warning", e.logs.String())
			}

			// A second click: the assignment is deleted already; nothing is cancelled again.
			if _, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver); !errors.Is(err, shared.ErrVersionConflict) {
				t.Fatalf("second delete at the old version = %v, want ErrVersionConflict", err)
			}
			if _, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver+1); !errors.Is(err, assignment.ErrDeleted) {
				t.Fatalf("second delete at the new version = %v, want ErrDeleted", err)
			}
			if got := e.cancel.calls(); len(got) != len(c.jobs) {
				t.Fatalf("cancelled %v after the second delete, want no more", got)
			}
		})
	}
}

// An email that may still go out or may have gone out unseen (a delivery in PENDING, SENDING or UNKNOWN: Busy) refuses
// the delete; nothing is read for the work, cancelled or changed. PENDING too: a delivery has no cancel.
func TestDeleteRefusedByAnEmailStopsNothing(t *testing.T) {
	e := newStopEnv(t)
	e.act.working[e.id], e.act.jobs[e.id] = true, []shared.JobID{"j-write"}
	e.busy[e.id] = true
	removals, syncs := withRemovals(e.createEnv)

	if _, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver); !errors.Is(err, assignment.ErrInUse) {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	if st := e.state(); !st.DeletedAt.IsZero() || st.Version != e.ver {
		t.Fatalf("a refused delete changed the assignment: %+v", st)
	}
	if got := e.cancel.calls(); len(got) != 0 || len(e.act.inTx) != 0 {
		t.Fatalf("cancelled %v, %d work reads; want none", got, len(e.act.inTx))
	}
	if len(removals.queued) != 0 || syncs.n != 0 {
		t.Fatalf("queued %v, %d syncs", removals.queued, syncs.n)
	}
}

// A cancel that fails never undoes the delete: it is logged (the handlers refuse the deleted assignment). A job that
// ended meanwhile had nothing to stop and is not logged.
func TestDeleteStaysWhenTheCancelFails(t *testing.T) {
	errCancel := errors.New("job j-write is RUNNING but not cancellable now")
	cases := []struct {
		name    string
		prepare func(e *stopEnv)
		warn    string // "" : no warning
	}{
		{name: "the cancel fails", warn: opStopFailed,
			prepare: func(e *stopEnv) { e.cancel.err = errCancel }},
		{name: "the job ended meanwhile",
			prepare: func(e *stopEnv) { e.cancel.err, e.cancel.ended = errCancel, true }},
		{name: "the jobs cannot be read", warn: opStopNotRead,
			prepare: func(e *stopEnv) { e.act.listErr = errBusyRead }},
		{name: "the jobs cannot be read again after a failed cancel", warn: opStopFailed,
			prepare: func(e *stopEnv) {
				e.cancel.err = errCancel
				e.cancel.check = func(context.Context) {
					e.act.mu.Lock()
					e.act.listErr = errBusyRead
					e.act.mu.Unlock()
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newStopEnv(t)
			e.act.jobs[e.id] = []shared.JobID{"j-write"}
			c.prepare(e)

			d, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver)
			if err != nil || !d.Assignment.Deleted || !d.Stopped {
				t.Fatalf("DeleteAndStop = %+v, %v; want deleted and stopped", d, err)
			}
			if e.state().DeletedAt.IsZero() {
				t.Fatal("the delete was undone")
			}
			logs := e.logs.String()
			switch {
			case c.warn == "" && strings.Contains(logs, "level=WARN"):
				t.Fatalf("log = %q, want no warning", logs)
			case c.warn != "" && !strings.Contains(logs, c.warn):
				t.Fatalf("log = %q, want %q", logs, c.warn)
			}
		})
	}
}

// A failed read of the work under way fails the delete before anything is stored.
func TestDeleteFailsWhenTheWorkCannotBeRead(t *testing.T) {
	e := newStopEnv(t)
	e.act.err = errBusyRead
	if _, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver); !errors.Is(err, errBusyRead) {
		t.Fatalf("err = %v, want the read error", err)
	}
	if st := e.state(); !st.DeletedAt.IsZero() || st.Version != e.ver || len(e.cancel.calls()) != 0 {
		t.Fatalf("a failed delete changed the assignment or cancelled: %+v, %v", st, e.cancel.calls())
	}
}

// Two clicks at once: one delete wins, the other sees another version; the job is cancelled once.
func TestConcurrentDeletesCancelOnce(t *testing.T) {
	e := newStopEnv(t)
	e.act.jobs[e.id] = []shared.JobID{"j-write"}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.svc.DeleteAndStop(context.Background(), e.id, e.ver) })
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, shared.ErrVersionConflict):
			t.Fatalf("losing delete = %v, want ErrVersionConflict", err)
		}
	}
	if ok != 1 || !slices.Equal(e.cancel.calls(), []shared.JobID{"j-write"}) {
		t.Fatalf("%d deletes won, cancelled %v; want one and one cancel", ok, e.cancel.calls())
	}
}

// «Вернуть» brings the assignment back and restarts nothing: no job is created or cancelled, the work stays stopped.
func TestRestoreRestartsNothing(t *testing.T) {
	e := newStopEnv(t)
	e.act.working[e.id], e.act.jobs[e.id] = true, []shared.JobID{"j-write"}
	d, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver)
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	jobs := len(e.jobs)
	e.mu.Unlock()

	if _, err := e.svc.RestoreAssignment(context.Background(), e.id, d.Assignment.Version); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	after := len(e.jobs)
	e.mu.Unlock()
	if after != jobs || len(e.cancel.calls()) != 1 {
		t.Fatalf("jobs %d -> %d, cancels %v; want nothing new", jobs, after, e.cancel.calls())
	}
	if got, _ := e.act.ActiveJobs(context.Background(), e.id); len(got) != 0 {
		t.Fatalf("unfinished jobs after the restore = %v, want none", got)
	}
}

func TestDeletionImpact(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(e *stopEnv) shared.AssignmentID
		want    bool
		err     error
	}{
		{name: "nothing under way", prepare: func(e *stopEnv) shared.AssignmentID { return e.id }},
		{name: "a work being written", want: true, prepare: func(e *stopEnv) shared.AssignmentID {
			e.act.working[e.id] = true
			return e.id
		}},
		{name: "a draft generation", want: true, prepare: func(e *stopEnv) shared.AssignmentID {
			e.act.jobs[e.id] = []shared.JobID{"j-draft"}
			return e.id
		}},
		{name: "an unknown id", prepare: func(e *stopEnv) shared.AssignmentID {
			e.act.working["nope"] = true
			return "nope"
		}},
		{name: "a deleted one", prepare: func(e *stopEnv) shared.AssignmentID {
			if _, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver); err != nil {
				e.t.Fatal(err)
			}
			e.act.working[e.id] = true
			return e.id
		}},
		{name: "no activity port", prepare: func(e *stopEnv) shared.AssignmentID {
			e.act.working[e.id] = true
			e.svc.activity = nil
			return e.id
		}},
		{name: "the read fails", err: errBusyRead, prepare: func(e *stopEnv) shared.AssignmentID {
			e.act.err = errBusyRead
			return e.id
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newStopEnv(t)
			id := c.prepare(e)
			got, err := e.svc.DeletionImpact(context.Background(), id)
			if !errors.Is(err, c.err) || (c.err == nil) != (err == nil) || got.Working != c.want {
				t.Fatalf("DeletionImpact = %+v, %v; want working %v, err %v", got, err, c.want, c.err)
			}
		})
	}
}

// Without the activity port (or the canceller) a delete stops nothing itself and never reports a stop.
func TestDeleteWithoutActivity(t *testing.T) {
	e := newStopEnv(t)
	e.act.jobs[e.id] = []shared.JobID{"j-write"}
	e.svc.activity, e.svc.jobs = nil, nil
	d, err := e.svc.DeleteAndStop(context.Background(), e.id, e.ver)
	if err != nil || d.Stopped || len(e.cancel.calls()) != 0 {
		t.Fatalf("DeleteAndStop = %+v, %v, cancels %v", d, err, e.cancel.calls())
	}
}

// A homework_draft job of a deleted assignment ends at once: no model call, no version (TASK-338).
func TestDraftJobOfADeletedAssignment(t *testing.T) {
	ctx := context.Background()
	e := newDraftEnv(t)
	e.llm.Answer(values.OperationHomeworkDraft, answer("Chapter 4", "Text."))
	res, err := e.svc.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	st := e.assignments["a1"]
	st.DeletedAt = ppT0.Add(time.Minute)
	e.assignments["a1"] = st
	e.mu.Unlock()

	err = e.svc.Handle(ctx, e.job(res.JobID).Snapshot(), nil)
	if !errors.Is(err, ErrDraftAssignmentGone) || !errors.Is(err, backoff.ErrPermanent) {
		t.Fatalf("Handle = %v, want permanent ErrDraftAssignmentGone", err)
	}
	if n := len(e.llm.Requests()); n != 0 {
		t.Fatalf("%d model calls, want none", n)
	}
	if _, err := e.drafts.Get(ctx, "a1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("draft stored: %v", err)
	}
}

// deletingLLM answers like the wrapped client and deletes the assignment during the call, as «Удалить» whose cancel
// did not reach the job would.
type deletingLLM struct {
	LLMClient
	del func()
}

func (d deletingLLM) Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error) {
	res, err := d.LLMClient.Complete(ctx, req)
	d.del()
	return res, err
}

// An assignment deleted while the model answers: the answer is dropped and the job ends.
func TestDraftAnswerOfAnAssignmentDeletedDuringTheCall(t *testing.T) {
	ctx := context.Background()
	e := newDraftEnv(t)
	e.llm.Answer(values.OperationHomeworkDraft, answer("Chapter 4", "Text."))
	svc, err := NewDrafts(e.draftDeps(deletingLLM{LLMClient: e.llm, del: func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		st := e.assignments["a1"]
		st.DeletedAt = ppT0.Add(time.Minute)
		e.assignments["a1"] = st
	}}))
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	res, err := e.svc.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}

	err = e.svc.Handle(ctx, e.job(res.JobID).Snapshot(), nil)
	if !errors.Is(err, ErrDraftAssignmentGone) || !errors.Is(err, backoff.ErrPermanent) {
		t.Fatalf("Handle = %v, want permanent ErrDraftAssignmentGone", err)
	}
	if n := len(e.llm.Requests()); n != 1 {
		t.Fatalf("%d model calls, want 1", n)
	}
	if _, err := e.drafts.Get(ctx, "a1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("the answer was stored: %v", err)
	}
}
