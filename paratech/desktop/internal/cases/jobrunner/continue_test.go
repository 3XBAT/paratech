package jobrunner

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
)

const typeSteps job.Type = "steps"

// TASK-097: a handler that stores one step and returns job.ErrContinueLater is QUEUED again with its attempt given
// back, so a job with one attempt runs all its steps; the progress of the steps stays and the last one completes it.
func TestContinueLaterRequeuesWithoutUsingAnAttempt(t *testing.T) {
	e := newEnv(t, nil, nil)
	const steps = 3
	var calls atomic.Int32
	e.register(typeSteps, func(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
		n := int(calls.Add(1))
		if want := fmt.Sprintf("%d/%d", n-1, steps); n > 1 && snap.Checkpoint() != want {
			return fmt.Errorf("step %d resumes from %q, want %q", n, snap.Checkpoint(), want)
		}
		p, err := job.NewProgress(n*100/steps, fmt.Sprintf("%d/%d", n, steps))
		if err != nil {
			return err
		}
		if err := report(ctx, p); err != nil {
			return err
		}
		if n < steps {
			return job.ErrContinueLater
		}
		return nil
	})
	id := e.enqueue(typeSteps, 1)
	e.start()

	e.waitStatus(id, job.StatusCompleted)
	if n := calls.Load(); n != steps {
		t.Fatalf("handler calls = %d, want %d", n, steps)
	}
	want := []job.Status{
		job.StatusQueued,
		job.StatusRunning, job.StatusRunning, job.StatusQueued,
		job.StatusRunning, job.StatusRunning, job.StatusQueued,
		job.StatusRunning, job.StatusRunning, job.StatusCompleted,
	}
	path := e.repo.path(id)
	if got := statuses(path); !equalStatuses(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	for _, s := range path {
		if s.Status == job.StatusQueued && (s.Attempts != 0 || s.LastError != "" || s.Delay != 0) {
			t.Fatalf("queued between steps with %d attempts, error %q, delay %v; want due at once, no attempt used",
				s.Attempts, s.LastError, s.Delay)
		}
	}
	if st := e.repo.snapshot(id); st.Attempts != 1 || st.Checkpoint != "3/3" {
		t.Fatalf("completed job: attempts %d checkpoint %q", st.Attempts, st.Checkpoint)
	}
}

// A user cancellation wins over a step that ends with job.ErrContinueLater: the job is CANCELLED, not queued again.
func TestCancelWinsOverContinueLater(t *testing.T) {
	e := newEnv(t, nil, nil)
	started := make(chan struct{})
	e.register(typeSteps, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-ctx.Done()
		return job.ErrContinueLater
	})
	id := e.enqueue(typeSteps, 3)
	e.start()
	<-started

	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	e.waitStatus(id, job.StatusCancelled)
}

// A step that returns job.ErrContinueLater after its timeout fired is still stored: the job continues.
func TestContinueLaterAfterTheTimeout(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	if err := e.svc.Register(Registration{
		Type: typeSteps, Recovery: RecoveryRequeue, Timeout: time.Second,
		Handler: handlerFunc(func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
			if calls.Add(1) > 1 {
				return nil
			}
			<-ctx.Done() // the timeout, once drive moves the fake clock
			return job.ErrContinueLater
		}),
	}); err != nil {
		t.Fatal(err)
	}
	id := e.enqueue(typeSteps, 1)
	e.start()

	e.drive("the job completes", func() bool { return e.status(id) == job.StatusCompleted })
	if st := e.repo.snapshot(id); st.Attempts != 1 {
		t.Fatalf("attempts = %d, want the step given back", st.Attempts)
	}
}
