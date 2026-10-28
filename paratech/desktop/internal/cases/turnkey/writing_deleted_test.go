package turnkey

import (
	"errors"
	"testing"

	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
)

// deleteA1 soft-deletes assignment a1 as «Удалить» does (TASK-338); restore brings it back.
func (e *writeEnv) deleteA1(t *testing.T, restore bool) {
	t.Helper()
	a, err := e.svc.assignments.Get(t.Context(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if restore {
		err = a.Restore(t0)
	} else {
		err = a.Delete(t0)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// A work_writing job of a deleted assignment ends at once (its delete may have failed to cancel it): before the call
// nothing is asked, during the call the answer is dropped, before the file nothing is filed (TASK-338).
func TestWritingOfADeletedAssignment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, e *writeEnv) (work.StepKind, *job.Job)
		calls int
	}{
		{
			name: "deleted before the outline",
			setup: func(t *testing.T, e *writeEnv) (work.StepKind, *job.Job) {
				st, err := e.svc.StartWriting(t.Context(), e.confirmed(t))
				if err != nil {
					t.Fatal(err)
				}
				e.deleteA1(t, false)
				return work.StepOutline, e.job(t, st.Job)
			},
		},
		{
			name: "deleted while the model writes the outline", calls: 1,
			setup: func(t *testing.T, e *writeEnv) (work.StepKind, *job.Job) {
				st, err := e.svc.StartWriting(t.Context(), e.confirmed(t))
				if err != nil {
					t.Fatal(err)
				}
				e.writer.answer = func(cvalues.LLMRequest) string {
					e.deleteA1(t, false)
					return outlineAnswer
				}
				return work.StepOutline, e.job(t, st.Job)
			},
		},
		{
			name: "deleted before the file",
			setup: func(t *testing.T, e *writeEnv) (work.StepKind, *job.Job) {
				id := e.confirmed(t)
				j := e.job(t, e.approved(t, id).Job)
				for range 20 {
					if w, _ := e.works.Get(t.Context(), id); w.Next().Kind == work.StepAssemble {
						break
					}
					if err := e.svc.Handle(t.Context(), j.Snapshot(), nil); !errors.Is(err, job.ErrContinueLater) {
						t.Fatalf("section: %v", err)
					}
				}
				e.deleteA1(t, false)
				return work.StepAssemble, j
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newWriteEnv(t)
			step, j := tc.setup(t, e)
			calls, version := len(e.writer.reqs), workVersion(t, e)

			err := e.svc.Handle(t.Context(), j.Snapshot(), nil)
			if !errors.Is(err, ErrWriteGone) || !errors.Is(err, backoff.ErrPermanent) {
				t.Fatalf("Handle = %v, want permanent ErrWriteGone", err)
			}
			if got := len(e.writer.reqs) - calls; got != tc.calls {
				t.Fatalf("%d model calls, want %d", got, tc.calls)
			}
			w, err := e.works.Get(t.Context(), "w1")
			if err != nil {
				t.Fatal(err)
			}
			if w.Version() != version || w.Next().Kind != step || w.Status() != work.StatusWriting {
				t.Fatalf("work = v%d %s next %s, want v%d unchanged at %s", w.Version(), w.Status(), w.Next().Kind, version, step)
			}
			if len(e.shelf.files) != 0 {
				t.Fatalf("filed %v, want nothing", e.shelf.files)
			}
		})
	}
}

// After «Вернуть» the work shows as stopped (writing, no job) and the student continues it with StartWriting: a new
// job, nothing asked again that was stored.
func TestWritingContinuesAfterTheRestore(t *testing.T) {
	e := newWriteEnv(t)
	ctx := t.Context()
	id := e.confirmed(t)
	st, err := e.svc.StartWriting(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	e.deleteA1(t, false)
	j := e.job(t, st.Job)
	if err := e.svc.Handle(ctx, j.Snapshot(), nil); !errors.Is(err, ErrWriteGone) {
		t.Fatalf("Handle = %v", err)
	}
	if err := errors.Join(j.Claim(t0), j.Fail(t0, "gone")); err != nil { // as the runner records it
		t.Fatal(err)
	}

	e.deleteA1(t, true)
	v, err := e.svc.Work(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != work.StatusWriting || v.Writing == nil || v.Writing.Active {
		t.Fatalf("view after the restore = %s %+v, want writing with no job", v.Status, v.Writing)
	}

	again, err := e.svc.StartWriting(ctx, id)
	if err != nil || !again.Created || again.Job == st.Job {
		t.Fatalf("StartWriting after the restore = %+v, %v; want a new job", again, err)
	}
	if steps, err := e.run(t, again.Job, 10); err != nil || steps != 1 {
		t.Fatalf("outline job = %v after %d steps", err, steps)
	}
}

func workVersion(t *testing.T, e *writeEnv) int64 {
	t.Helper()
	w, err := e.works.Get(t.Context(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	return w.Version()
}
