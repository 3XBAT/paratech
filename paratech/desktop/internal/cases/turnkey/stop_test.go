package turnkey

import (
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// TestStopWriting: «Остановить» cancels the running writing job and keeps the sections written; a finished job or
// none is no error and nothing is cancelled.
func TestStopWriting(t *testing.T) {
	errRefused := errors.New("not cancellable")
	for _, tc := range []struct {
		name string
		// setup returns the work and the job of its writing ("" none) after moving it as the case needs.
		setup      func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID)
		before     func(j *job.Job)
		refuse     error
		wantErr    error
		wantCalls  int
		wantStatus job.Status // of the job after the stop; "" no job
	}{
		{
			name: "queued sections job", wantCalls: 1, wantStatus: job.StatusCancelled,
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				return id, e.approved(t, id).Job
			},
		},
		{
			name: "running after a section", wantCalls: 1, wantStatus: job.StatusCancelled,
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				ap := e.approved(t, id)
				j := e.job(t, ap.Job)
				if err := e.svc.Handle(t.Context(), j.Snapshot(), nil); !errors.Is(err, job.ErrContinueLater) {
					t.Fatalf("first section: %v", err)
				}
				if err := j.Claim(t0); err != nil {
					t.Fatal(err)
				}
				return id, ap.Job
			},
		},
		{
			name: "waiting for the network", wantCalls: 1, wantStatus: job.StatusCancelled,
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				ap := e.approved(t, id)
				j := e.job(t, ap.Job)
				if err := errors.Join(j.Claim(t0), j.WaitFor(job.StatusWaitingForNetwork, t0)); err != nil {
					t.Fatal(err)
				}
				return id, ap.Job
			},
		},
		{
			name: "finished job", wantStatus: job.StatusCompleted,
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				ap := e.approved(t, id)
				if _, err := e.run(t, ap.Job, 10); err != nil {
					t.Fatal(err)
				}
				return id, ap.Job
			},
		},
		{
			name: "no job",
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				return e.confirmed(t), ""
			},
		},
		{
			name: "finished while stopped", wantCalls: 1, wantStatus: job.StatusCompleted, refuse: errRefused,
			before: func(j *job.Job) { _ = errors.Join(j.Claim(t0), j.Complete(t0)) },
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				return id, e.approved(t, id).Job
			},
		},
		{
			name: "running with no handler", wantCalls: 1, wantStatus: job.StatusRunning, refuse: errRefused,
			wantErr: ErrNotStopped,
			setup: func(t *testing.T, e *writeEnv) (shared.WorkID, shared.JobID) {
				id := e.confirmed(t)
				ap := e.approved(t, id)
				if err := e.job(t, ap.Job).Claim(t0); err != nil {
					t.Fatal(err)
				}
				return id, ap.Job
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newWriteEnv(t)
			ctx := t.Context()
			id, jid := tc.setup(t, e)
			e.canceller.before, e.canceller.err = tc.before, tc.refuse
			before, err := e.svc.Work(ctx, id)
			if err != nil {
				t.Fatal(err)
			}

			v, err := e.svc.StopWriting(ctx, id)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("StopWriting = %v, want %v", err, tc.wantErr)
			}
			if len(e.canceller.calls) != tc.wantCalls {
				t.Fatalf("cancelled %v, want %d calls", e.canceller.calls, tc.wantCalls)
			}
			if jid != "" {
				if got := e.job(t, jid).Status(); got != tc.wantStatus {
					t.Fatalf("job = %s, want %s", got, tc.wantStatus)
				}
			}
			if tc.wantErr != nil {
				return
			}
			if v.Version != before.Version || v.Status != before.Status {
				t.Fatalf("the work changed: %d %s -> %d %s", before.Version, before.Status, v.Version, v.Status)
			}
			if w := v.Writing; w != nil && (w.Active || w.Job != "" || w.JobState != "") {
				t.Fatalf("still active after the stop: %+v", w)
			}
		})
	}
}

func TestStopWritingNotConfigured(t *testing.T) {
	e := newWriteEnv(t)
	id := e.confirmed(t)
	e.approved(t, id)
	e.svc.writing.Canceller = nil
	if _, err := e.svc.StopWriting(t.Context(), id); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no canceller: %v", err)
	}
	if _, err := newEnv(t).svc.StopWriting(t.Context(), id); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no writing: %v", err)
	}
}

// TestStopKeepsSections: the sections written before the stop stay, and the writing goes on or is regenerated.
func TestStopKeepsSections(t *testing.T) {
	e := newWriteEnv(t)
	ctx := t.Context()
	id := e.confirmed(t)
	ap := e.approved(t, id)
	j := e.job(t, ap.Job)
	if err := e.svc.Handle(ctx, j.Snapshot(), nil); !errors.Is(err, job.ErrContinueLater) {
		t.Fatalf("first section: %v", err)
	}
	if _, err := e.svc.Regenerate(ctx, id, 1, "Короче"); !errors.Is(err, ErrWritingRunning) {
		t.Fatalf("regenerate while the job runs: %v", err)
	}

	v, err := e.svc.StopWriting(ctx, id)
	if err != nil || v.Writing == nil || !v.Writing.Sections[0].Written || v.Writing.Done != 1 || v.Writing.Active {
		t.Fatalf("StopWriting = %+v, %v", v.Writing, err)
	}
	paid := e.writer.paid()

	re, err := e.svc.Regenerate(ctx, id, 2, "Подробнее")
	if err != nil || !re.Created || re.Job == ap.Job || !re.View.Writing.Active || re.View.Writing.Job != re.Job {
		t.Fatalf("Regenerate after the stop = %+v, %v", re, err)
	}
	if steps, err := e.run(t, re.Job, 10); err != nil || steps != 3 {
		t.Fatalf("the writing after the stop = %v after %d steps", err, steps)
	}
	// Section 1 is not asked again; 2 (regenerated) and 3 are.
	if got := e.writer.paid() - paid; got != 2 {
		t.Fatalf("paid after the stop = %d, want 2", got)
	}
	if v, err := e.svc.Work(ctx, id); err != nil || v.Status != work.StatusDone || v.Writing.Active {
		t.Fatalf("after the writing = %+v, %v", v, err)
	}
}

// TestWritingJobState: every read of a work shows the writing job that runs for it and its state.
func TestWritingJobState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		move       func(j *job.Job) error
		wantActive bool
		wantState  job.Status
	}{
		{"queued", func(*job.Job) error { return nil }, true, job.StatusQueued},
		{"running", func(j *job.Job) error { return j.Claim(t0) }, true, job.StatusRunning},
		{"waiting for the network", func(j *job.Job) error {
			return errors.Join(j.Claim(t0), j.WaitFor(job.StatusWaitingForNetwork, t0))
		}, true, job.StatusWaitingForNetwork},
		{"waiting for the quota", func(j *job.Job) error {
			return errors.Join(j.Claim(t0), j.WaitFor(job.StatusWaitingForQuota, t0))
		}, true, job.StatusWaitingForQuota},
		{"waiting for power", func(j *job.Job) error { return j.WaitFor(job.StatusWaitingForPower, t0) }, true,
			job.StatusWaitingForPower},
		{"retrying", func(j *job.Job) error {
			return errors.Join(j.Claim(t0), j.Retry(t0, t0.Add(time.Minute), "turnkey writing: the model call failed"))
		}, true, job.StatusRetryWait},
		{"completed", func(j *job.Job) error { return errors.Join(j.Claim(t0), j.Complete(t0)) }, false, ""},
		{"failed", func(j *job.Job) error { return errors.Join(j.Claim(t0), j.Fail(t0, "failed")) }, false, ""},
		{"cancelled", func(j *job.Job) error { return j.Cancel(t0) }, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newWriteEnv(t)
			ctx := t.Context()
			id := e.confirmed(t)
			ap := e.approved(t, id)
			if err := tc.move(e.job(t, ap.Job)); err != nil {
				t.Fatal(err)
			}

			v, err := e.svc.Work(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			w := v.Writing
			wantJob := shared.JobID("")
			if tc.wantActive {
				wantJob = ap.Job
			}
			if w.Active != tc.wantActive || w.JobState != tc.wantState || w.Job != wantJob {
				t.Fatalf("writing = active %v %q %q, want %v %q %q", w.Active, w.JobState, w.Job, tc.wantActive,
					tc.wantState, wantJob)
			}
			if of, err := e.svc.WorkOf(ctx, work.Source{Assignment: "a1"}); err != nil || of.Writing.Active != tc.wantActive {
				t.Fatalf("WorkOf = %+v, %v", of.Writing, err)
			}
		})
	}
}

// TestEditOutlineAt: an editor at a stale version writes nothing; the outline as it is keeps the version.
func TestEditOutlineAt(t *testing.T) {
	e := newWriteEnv(t)
	ctx := t.Context()
	id := e.confirmed(t)
	st, err := e.svc.StartWriting(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, st.Job, 10); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Work(ctx, id)
	if err != nil || v.Writing.Step != work.StepApprove {
		t.Fatalf("outline = %+v, %v", v.Writing, err)
	}
	same := make([]work.OutlineItem, 0, len(v.Writing.Sections))
	for _, s := range v.Writing.Sections {
		same = append(same, work.OutlineItem{Number: s.Number, Title: s.Title, Words: s.Words})
	}
	edited := append([]work.OutlineItem(nil), same...)
	edited[1].Words = 900

	// The outline as it is: nothing saved, by either call.
	for name, call := range map[string]func() (View, error){
		"EditOutline":   func() (View, error) { return e.svc.EditOutline(ctx, id, same) },
		"EditOutlineAt": func() (View, error) { return e.svc.EditOutlineAt(ctx, id, v.Version, same) },
	} {
		if got, err := call(); err != nil || got.Version != v.Version {
			t.Fatalf("%s without a change = version %d, %v; want %d", name, got.Version, err, v.Version)
		}
	}

	// A stale version: refused, nothing written.
	if _, err := e.svc.EditOutlineAt(ctx, id, v.Version-1, edited); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale version: %v", err)
	}
	if got, _ := e.svc.Work(ctx, id); got.Version != v.Version || got.Writing.Sections[1].Words == 900 {
		t.Fatalf("a stale edit was written: %+v", got.Writing.Sections)
	}

	// Another save between the read and the save of the edit: refused too, the other save stands.
	e.works.beforeSave = func() {
		w, err := e.works.Get(ctx, id)
		if err == nil {
			err = e.works.Save(ctx, w)
		}
		if err != nil {
			t.Error(err)
		}
	}
	if _, err := e.svc.EditOutlineAt(ctx, id, v.Version, edited); !errors.Is(err, ErrChanged) {
		t.Fatalf("lost save: %v", err)
	}
	cur, err := e.svc.Work(ctx, id)
	if err != nil || cur.Version == v.Version || cur.Writing.Sections[1].Words == 900 {
		t.Fatalf("after the lost save = %+v, %v", cur, err)
	}

	// The current version: the edit is kept.
	got, err := e.svc.EditOutlineAt(ctx, id, cur.Version, edited)
	if err != nil || got.Version == cur.Version || got.Writing.Sections[1].Words != 900 {
		t.Fatalf("EditOutlineAt = %+v, %v", got, err)
	}
}
