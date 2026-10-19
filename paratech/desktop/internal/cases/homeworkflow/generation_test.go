package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// TASK-094: while a generation of the assignment is pending or running, another one is refused; the same request
// returns the running job; once it is over a new one starts.
func TestGenerateRefusesASecondGenerationWhileOneIsActive(t *testing.T) {
	active := []job.Status{
		job.StatusQueued, job.StatusRunning, job.StatusRetryWait, job.StatusWaitingForNetwork, job.StatusWaitingForQuota,
		job.StatusWaitingForPower,
	}
	for _, st := range active {
		t.Run(string(st), func(t *testing.T) {
			e := newDraftEnv(t)
			ctx := context.Background()
			first, err := e.svc.Generate(ctx, "a1", "shorter")
			if err != nil {
				t.Fatal(err)
			}
			e.settle(first.JobID, st)

			if _, err := e.svc.Generate(ctx, "a1", "longer"); !errors.Is(err, ErrGenerationRunning) {
				t.Fatalf("other instructions: err = %v, want ErrGenerationRunning", err)
			}
			if _, err := e.svc.GenerateFrom(ctx, "a1", 0, ""); !errors.Is(err, ErrGenerationRunning) {
				t.Fatalf("no instructions: err = %v, want ErrGenerationRunning", err)
			}
			same, err := e.svc.Generate(ctx, "a1", "shorter")
			if err != nil || same.Created || same.JobID != first.JobID {
				t.Fatalf("same request: %+v, %v", same, err)
			}
			if e.jobCount() != 1 {
				t.Fatalf("jobs = %d, want 1", e.jobCount())
			}
		})
	}
	for _, end := range []job.Status{job.StatusFailed, job.StatusCancelled, job.StatusCompleted} {
		t.Run("after "+string(end), func(t *testing.T) {
			e := newDraftEnv(t)
			ctx := context.Background()
			first, err := e.svc.Generate(ctx, "a1", "shorter")
			if err != nil {
				t.Fatal(err)
			}
			e.settle(first.JobID, end)
			next, err := e.svc.Generate(ctx, "a1", "longer")
			if err != nil || !next.Created {
				t.Fatalf("after the first ended: %+v, %v", next, err)
			}
		})
	}
}

// Another assignment is not blocked by the generation of the first.
func TestGenerationOfAnotherAssignmentIsNotBlocked(t *testing.T) {
	e := newDraftEnv(t)
	e.addAssignment("a10", "Other", "Other homework", t0)
	ctx := context.Background()
	if _, err := e.svc.Generate(ctx, "a1", ""); err != nil {
		t.Fatal(err)
	}
	// "a10" starts with "a1": the key prefix ends with a separator, so it is not a job of a1.
	if res, err := e.svc.Generate(ctx, "a10", "longer"); err != nil || !res.Created {
		t.Fatalf("a10: %+v, %v", res, err)
	}
}

// Concurrent starts with different instructions make exactly one active job; the others are refused or get it.
func TestConcurrentDifferentGenerationsMakeOneJob(t *testing.T) {
	e := newDraftEnv(t)
	ctx := context.Background()
	const clicks = 8
	results := make([]GenerateResult, clicks)
	errs := make([]error, clicks)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range clicks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = e.svc.Generate(ctx, "a1", fmt.Sprintf("variant %d", i%2))
		}()
	}
	close(start)
	wg.Wait()

	created := 0
	for i := range clicks {
		switch {
		case errs[i] == nil && results[i].Created:
			created++
		case errs[i] == nil:
		case !errors.Is(errs[i], ErrGenerationRunning):
			t.Fatalf("click %d: %v", i, errs[i])
		}
	}
	if created != 1 || e.jobCount() != 1 {
		t.Fatalf("created = %d, jobs = %d; want 1 and 1", created, e.jobCount())
	}
}

func TestActiveGeneration(t *testing.T) {
	e := newDraftEnv(t)
	ctx := context.Background()
	if _, ok, err := e.svc.ActiveGeneration(ctx, "a1"); err != nil || ok {
		t.Fatalf("before any generation: ok %v, %v", ok, err)
	}
	if _, _, err := e.svc.ActiveGeneration(ctx, "missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}

	res, err := e.svc.Generate(ctx, "a1", "")
	if err != nil {
		t.Fatal(err)
	}
	v, ok, err := e.svc.ActiveGeneration(ctx, "a1")
	if err != nil || !ok || v.JobID != res.JobID || v.Status != job.StatusQueued {
		t.Fatalf("queued: %+v, %v, %v", v, ok, err)
	}
	e.settle(res.JobID, job.StatusRunning)
	pr, err := job.NewProgress(answeredPercent, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.job(res.JobID).ReportProgress(pr, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := e.svc.ActiveGeneration(ctx, "a1"); err != nil || !ok || v.Status != job.StatusRunning || v.Percent != answeredPercent {
		t.Fatalf("running: %+v, %v, %v", v, ok, err)
	}
	if g, err := e.svc.Generation(ctx, res.JobID); err != nil || g.Percent != answeredPercent {
		t.Fatalf("Generation = %+v, %v", g, err)
	}

	e.llm.Answer(values.OperationHomeworkDraft, answer("T", "Body."))
	if err := e.svc.Handle(ctx, e.job(res.JobID).Snapshot(), nil); err != nil {
		t.Fatal(err)
	}
	if err := e.job(res.JobID).Complete(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.svc.ActiveGeneration(ctx, "a1"); err != nil || ok {
		t.Fatalf("after the version: ok %v, %v", ok, err)
	}
}
