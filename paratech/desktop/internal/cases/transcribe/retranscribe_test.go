package transcribe

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/services/modelselection"
)

// autoJob enqueues the automatic STT job of rec-1 and moves it to s; a failure is an engine failure.
func (e *enqueueEnv) autoJob(t *testing.T, s job.Status) *job.Job {
	t.Helper()
	return e.autoJobFailing(t, s, ErrTranscriptionFailed.Error())
}

// autoJobFailing is autoJob with the last error of a failed or retried job.
func (e *enqueueEnv) autoJobFailing(t *testing.T, s job.Status, lastError string) *job.Job {
	t.Helper()
	if err := e.enq.RecordingCompleted(context.Background(), "rec-1"); err != nil {
		t.Fatal(err)
	}
	jobs := e.jobs.all()
	if len(jobs) != 1 {
		t.Fatalf("%d jobs, want the automatic one", len(jobs))
	}
	j := jobs[0]
	now := e.clock.Now().Add(time.Second)
	switch s {
	case job.StatusQueued:
	case job.StatusRunning:
		must(t, j.Claim(now))
	case job.StatusCancelled:
		must(t, j.Cancel(now))
	case job.StatusFailed:
		must(t, j.Claim(now))
		must(t, j.Fail(now, lastError))
	case job.StatusRetryWait:
		must(t, j.Claim(now))
		must(t, j.Retry(now, now.Add(time.Minute), lastError))
	case job.StatusCompleted:
		must(t, j.Claim(now))
		must(t, j.Complete(now))
	default:
		t.Fatalf("no step to %s", s)
	}
	e.clock.Advance(time.Minute)
	return j
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// A cancelled transcription starts again as a new job pinned to the current model, in one transaction; a repeated
// call makes no second job, and the automatic enqueue still makes none.
func TestRetranscribeAfterCancel(t *testing.T) {
	e := newEnqueueEnv(t)
	ctx := context.Background()
	e.autoJob(t, job.StatusCancelled)
	e.models.model = "m-2" // the default changed meanwhile: the new job pins it
	txBefore := e.tx.calls

	id, err := e.enq.Retranscribe(ctx, "rec-1")
	if err != nil {
		t.Fatalf("Retranscribe: %v", err)
	}
	var created *job.Job
	for _, j := range e.jobs.all() {
		if j.ID() == id {
			created = j
		}
	}
	if created == nil || created.Key() != "stt:rec-1:m-2:r2" || created.PinnedModelID() != "m-2" ||
		created.Status() != job.StatusQueued || created.MaxAttempts() != 4 || !created.CreatedAt().Equal(e.clock.Now()) {
		t.Fatalf("new job %+v", created)
	}
	if e.tx.calls != txBefore+1 || e.models.outsideTx != 0 || e.jobs.outsideTx != 0 {
		t.Fatalf("%d transactions, %d resolves and %d job calls outside one", e.tx.calls-txBefore, e.models.outsideTx,
			e.jobs.outsideTx)
	}

	active, ok, err := e.enq.ActiveJob(ctx, "rec-1")
	if err != nil || !ok || active != id {
		t.Fatalf("ActiveJob = %q, %v, %v; want %q", active, ok, err, id)
	}

	if _, err := e.enq.Retranscribe(ctx, "rec-1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("repeated Retranscribe = %v, want ErrNotAllowed", err)
	}
	if err := e.enq.RecordingCompleted(ctx, "rec-1"); err != nil {
		t.Fatal(err)
	}
	e.pending.ids = []shared.RecordingID{"rec-1"}
	if n, err := e.enq.EnqueuePending(ctx); err != nil || n != 0 {
		t.Fatalf("EnqueuePending = %d, %v; want nothing", n, err)
	}
	if n := len(e.jobs.all()); n != 2 {
		t.Fatalf("%d jobs, want the cancelled one and the new one", n)
	}
}

// TASK-409: a transcription that failed finally starts again like a cancelled one; a repeated call makes no second job.
func TestRetranscribeAfterFailure(t *testing.T) {
	e := newEnqueueEnv(t)
	ctx := context.Background()
	e.autoJob(t, job.StatusFailed)

	id, err := e.enq.Retranscribe(ctx, "rec-1")
	if err != nil {
		t.Fatalf("Retranscribe: %v", err)
	}
	var created *job.Job
	for _, j := range e.jobs.all() {
		if j.ID() == id {
			created = j
		}
	}
	if created == nil || created.Key() != "stt:rec-1:m-1:r2" || created.Status() != job.StatusQueued {
		t.Fatalf("new job %+v", created)
	}
	if _, err := e.enq.Retranscribe(ctx, "rec-1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("repeated Retranscribe = %v, want ErrNotAllowed", err)
	}
	if n := len(e.jobs.all()); n != 2 {
		t.Fatalf("%d jobs, want the failed one and the new one", n)
	}
}

// The second cancel of a recording gets the next key.
func TestRetranscribeCountsEveryJob(t *testing.T) {
	e := newEnqueueEnv(t)
	ctx := context.Background()
	e.autoJob(t, job.StatusCancelled)
	id, err := e.enq.Retranscribe(ctx, "rec-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range e.jobs.all() {
		if j.ID() == id {
			must(t, j.Cancel(e.clock.Now()))
		}
	}
	e.clock.Advance(time.Minute)

	id, err = e.enq.Retranscribe(ctx, "rec-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range e.jobs.all() {
		if j.ID() == id && j.Key() != "stt:rec-1:m-1:r3" {
			t.Fatalf("key %q, want stt:rec-1:m-1:r3", j.Key())
		}
	}
}

// Retranscribe refuses unless the newest STT job was cancelled or failed finally (not for lack of speech) and nothing
// else stands in the way.
func TestRetranscribeRefuses(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, e *enqueueEnv)
	}{
		{"no job yet", func(*testing.T, *enqueueEnv) {}},
		{"queued", func(t *testing.T, e *enqueueEnv) { e.autoJob(t, job.StatusQueued) }},
		{"running", func(t *testing.T, e *enqueueEnv) { e.autoJob(t, job.StatusRunning) }},
		{"a retry is pending", func(t *testing.T, e *enqueueEnv) { e.autoJob(t, job.StatusRetryWait) }},
		{"failed for lack of speech", func(t *testing.T, e *enqueueEnv) {
			e.autoJobFailing(t, job.StatusFailed, ErrNoSpeech.Error())
		}},
		{"completed", func(t *testing.T, e *enqueueEnv) { e.autoJob(t, job.StatusCompleted) }},
		{"failed, with a transcript", func(t *testing.T, e *enqueueEnv) {
			e.autoJob(t, job.StatusFailed)
			e.transcript.has = map[shared.RecordingID]bool{"rec-1": true}
		}},
		{"a transcript exists", func(t *testing.T, e *enqueueEnv) {
			e.autoJob(t, job.StatusCancelled)
			e.transcript.has = map[shared.RecordingID]bool{"rec-1": true}
		}},
		{"the recording is gone", func(t *testing.T, e *enqueueEnv) {
			e.autoJob(t, job.StatusCancelled)
			delete(e.recordings.rows, "rec-1")
		}},
		{"the recording is not completed", func(t *testing.T, e *enqueueEnv) {
			e.autoJob(t, job.StatusCancelled)
			e.recordings.rows["rec-1"] = startedRecording(t, "rec-1")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			tt.setup(t, e)
			before := len(e.jobs.all())
			calls := e.models.calls
			if _, err := e.enq.Retranscribe(context.Background(), "rec-1"); !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("Retranscribe = %v, want ErrNotAllowed", err)
			}
			if n := len(e.jobs.all()); n != before {
				t.Fatalf("%d jobs, want %d", n, before)
			}
			if e.models.calls != calls {
				t.Fatal("a refused request resolved a model")
			}
		})
	}
}

// Without a model to pin nothing is created and the error says so.
func TestRetranscribeWithoutModel(t *testing.T) {
	for _, cause := range []error{modelselection.ErrModelNotInstalled, modelselection.ErrNoDefaultModel} {
		t.Run(cause.Error(), func(t *testing.T) {
			e := newEnqueueEnv(t)
			e.autoJob(t, job.StatusCancelled)
			e.models.err = cause
			if _, err := e.enq.Retranscribe(context.Background(), "rec-1"); !errors.Is(err, ErrNoModel) || !errors.Is(err, cause) {
				t.Fatalf("Retranscribe = %v, want ErrNoModel wrapping %v", err, cause)
			}
			if n := len(e.jobs.all()); n != 1 {
				t.Fatalf("%d jobs, want only the cancelled one", n)
			}
		})
	}
}

// A failing read or store is an error of its own, not a refusal.
func TestRetranscribeStorageErrors(t *testing.T) {
	boom := errors.New("disk I/O error")
	tests := []struct {
		name string
		set  func(e *enqueueEnv)
	}{
		{"list", func(e *enqueueEnv) { e.jobs.listErr = boom }},
		{"transcript", func(e *enqueueEnv) { e.transcript.err = boom }},
		{"model", func(e *enqueueEnv) { e.models.err = boom }},
		{"enqueue", func(e *enqueueEnv) { e.jobs.enqueueErr = boom }},
		{"recording", func(e *enqueueEnv) { e.recordings.err = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnqueueEnv(t)
			e.autoJob(t, job.StatusCancelled)
			tt.set(e)
			_, err := e.enq.Retranscribe(context.Background(), "rec-1")
			if !errors.Is(err, boom) || errors.Is(err, ErrNotAllowed) || errors.Is(err, ErrNoModel) {
				t.Fatalf("Retranscribe = %v, want the storage error", err)
			}
		})
	}
}

// ActiveJob finds the unfinished STT job of the recording, nothing once it is finished.
func TestActiveJob(t *testing.T) {
	tests := []struct {
		status job.Status
		active bool
	}{
		{job.StatusQueued, true},
		{job.StatusRunning, true},
		{job.StatusCancelled, false},
		{job.StatusRetryWait, true},
		{job.StatusFailed, false},
		{job.StatusCompleted, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			e := newEnqueueEnv(t)
			j := e.autoJob(t, tt.status)
			id, ok, err := e.enq.ActiveJob(context.Background(), "rec-1")
			if err != nil || ok != tt.active || (ok && id != j.ID()) {
				t.Fatalf("ActiveJob = %q, %v, %v; want active %v", id, ok, err, tt.active)
			}
		})
	}

	e := newEnqueueEnv(t)
	if _, ok, err := e.enq.ActiveJob(context.Background(), "rec-2"); err != nil || ok {
		t.Fatalf("ActiveJob of a recording without jobs = %v, %v", ok, err)
	}
	e.jobs.listErr = errors.New("disk I/O error")
	if _, _, err := e.enq.ActiveJob(context.Background(), "rec-1"); err == nil {
		t.Fatal("ActiveJob hid the storage error")
	}
}
