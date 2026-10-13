package tests

// Crash emulation for startup recovery (TASK-010, AC-09, AC-15, hard rule 7) on the real SQLite adapter: a first
// process claims jobs and dies, a second process opens the same file, recovers and runs. Only the clock is fake.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const (
	recoveryStt    job.Type = "stt"
	recoveryEmail  job.Type = "email_delivery"
	recoveryPush   job.Type = "export_push" // side effect whose reconciler does not exist yet
	recoveryLegacy job.Type = "legacy"      // no registration in the new build

	probeSent    = "sent"
	probeNotSent = "not_sent"
	probeUnknown = "unknown"

	reasonUnknown = "delivery outcome unknown"
)

// crashRunning enqueues a job and claims it, leaving it RUNNING as a process that dies mid-attempt does.
func crashRunning(t *testing.T, repo *sqlite.JobRepository, clock *testenv.FakeClock, id shared.JobID, typ job.Type, maxAttempts int, probe string) {
	t.Helper()
	payload := "{}"
	if probe != "" {
		payload = fmt.Sprintf(`{"probe":%q}`, probe)
	}
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: string(id), Payload: payload, MaxAttempts: maxAttempts}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, created, err := repo.Enqueue(ctx, j); err != nil || !created {
		t.Fatalf("Enqueue %s: created=%v err=%v", id, created, err)
	}
	filter, err := job.NewClaimFilter([]job.Type{typ}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := repo.Claim(ctx, clock.Now(), filter)
	if err != nil || !ok || claimed.ID() != id {
		t.Fatalf("Claim %s: %v ok=%v err=%v", id, claimed, ok, err)
	}
}

// probeReconciler answers from the payload, like a delivery use case that looked its delivery up.
type probeReconciler struct{}

func (probeReconciler) Reconcile(_ context.Context, snap job.Snapshot) (job.Status, string, error) {
	var p struct{ Probe string }
	if err := json.Unmarshal([]byte(snap.Payload()), &p); err != nil {
		return "", "", err
	}
	switch p.Probe {
	case probeSent:
		return job.StatusCompleted, "", nil
	case probeNotSent:
		return job.StatusQueued, "", nil
	}
	return job.StatusFailed, reasonUnknown, nil
}

// callLog counts handler calls per job.
type callLog struct {
	mu    sync.Mutex
	calls map[shared.JobID][]int // attempt numbers
}

func (c *callLog) handler() jobrunner.Handler {
	return handlerFn(func(_ context.Context, snap job.Snapshot, _ progressFn) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.calls[snap.ID()] = append(c.calls[snap.ID()], snap.Attempt())
		return nil
	})
}

func (c *callLog) of(id shared.JobID) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls[id])
}

func TestStartupRecoveryAfterACrash(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	path := runnerDB(t)
	clock := testenv.NewFakeClock(runnerT0)
	ctx := context.Background()

	// First process: claims jobs and dies without finishing any of them.
	first := openRunnerDB(t, path)
	repo := sqlite.NewJobRepository(first)
	crashRunning(t, repo, clock, "stt-1", recoveryStt, 3, "")
	crashRunning(t, repo, clock, "stt-exhausted", recoveryStt, 1, "")
	crashRunning(t, repo, clock, "stt-corrupt", recoveryStt, 3, "")
	crashRunning(t, repo, clock, "mail-sent", recoveryEmail, 3, probeSent)
	crashRunning(t, repo, clock, "mail-not-sent", recoveryEmail, 3, probeNotSent)
	crashRunning(t, repo, clock, "mail-unknown", recoveryEmail, 3, probeUnknown)
	crashRunning(t, repo, clock, "push-1", recoveryPush, 3, "")
	crashRunning(t, repo, clock, "legacy-1", recoveryLegacy, 3, "")
	queued, err := job.New(job.Spec{ID: "stt-queued", Type: recoveryStt, Key: "stt-queued", MaxAttempts: 3}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Enqueue(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if _, err := first.DBTX(ctx).ExecContext(ctx, "UPDATE jobs SET started_at = NULL WHERE id = 'stt-corrupt'"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Second process on the same file.
	clock.Advance(time.Minute)
	db := openRunnerDB(t, path)
	repo = sqlite.NewJobRepository(db)
	svc, err := jobrunner.New(jobrunner.Deps{Jobs: repo, Clock: clock, Sleeper: clock}, runnerConfig(2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	calls := &callLog{calls: map[shared.JobID][]int{}}
	for _, reg := range []jobrunner.Registration{
		{Type: recoveryStt, Recovery: jobrunner.RecoveryRequeue},
		{Type: recoveryEmail, Recovery: jobrunner.RecoveryReconcile, Reconciler: probeReconciler{}},
		{Type: recoveryPush, Recovery: jobrunner.RecoveryReconcile},
	} {
		reg.Handler = calls.handler()
		if err := svc.Register(reg); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := svc.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	wantReport := jobrunner.RecoveryReport{
		Requeued:     []shared.JobID{"stt-1", "mail-not-sent"},
		Completed:    []shared.JobID{"mail-sent"},
		Failed:       []shared.JobID{"stt-exhausted", "mail-unknown", "push-1"},
		Unregistered: []shared.JobID{"legacy-1"},
		Corrupt:      []shared.JobID{"stt-corrupt"},
	}
	if fmt.Sprint(sortedReport(rep)) != fmt.Sprint(sortedReport(wantReport)) {
		t.Fatalf("report:\n got  %+v\n want %+v", rep, wantReport)
	}

	get := func(id shared.JobID) *job.Job {
		t.Helper()
		j, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		return j
	}
	states := []struct {
		id       shared.JobID
		status   job.Status
		attempts int
		lastErr  string
	}{
		{"stt-1", job.StatusQueued, 1, ""},
		{"stt-exhausted", job.StatusFailed, 1, "attempts exhausted"},
		{"mail-sent", job.StatusCompleted, 1, ""},
		{"mail-not-sent", job.StatusQueued, 1, ""},
		{"mail-unknown", job.StatusFailed, 1, reasonUnknown},
		{"push-1", job.StatusFailed, 1, "interrupted with an unknown outcome"},
		{"legacy-1", job.StatusRunning, 1, ""},
		{"stt-queued", job.StatusQueued, 0, ""},
	}
	for _, w := range states {
		j := get(w.id)
		if j.Status() != w.status || j.Attempts() != w.attempts || !strings.HasPrefix(j.LastError(), w.lastErr) {
			t.Errorf("%s: %s attempts=%d %q, want %s attempts=%d %q", w.id, j.Status(), j.Attempts(), j.LastError(), w.status, w.attempts, w.lastErr)
		}
	}
	var corruptStatus string
	if err := db.DBTX(ctx).QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = 'stt-corrupt'").Scan(&corruptStatus); err != nil || corruptStatus != "RUNNING" {
		t.Fatalf("corrupt row: %q %v, want it left RUNNING", corruptStatus, err)
	}

	// A second recovery changes nothing.
	again, err := svc.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sortedReport(again)) != fmt.Sprint(sortedReport(jobrunner.RecoveryReport{
		Unregistered: wantReport.Unregistered, Corrupt: wantReport.Corrupt,
	})) {
		t.Fatalf("second Recover: %+v", again)
	}

	// The workers run what was requeued and nothing else.
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []shared.JobID{"stt-1", "mail-not-sent", "stt-queued"} {
		testenv.Eventually(t, string(id)+" completes", func() bool { return get(id).Status() == job.StatusCompleted },
			func() { clock.Advance(time.Second) })
	}
	wantCalls := map[shared.JobID]string{
		"stt-1": "[2]", "mail-not-sent": "[2]", "stt-queued": "[1]",
		"stt-exhausted": "[]", "mail-sent": "[]", "mail-unknown": "[]", "push-1": "[]", "legacy-1": "[]", "stt-corrupt": "[]",
	}
	for id, want := range wantCalls {
		if got := fmt.Sprint(calls.of(id)); got != want {
			t.Errorf("handler calls of %s (attempt numbers) = %s, want %s", id, got, want)
		}
	}
}

// sortedReport makes reports comparable regardless of listing order; errors are compared by job id.
func sortedReport(r jobrunner.RecoveryReport) [][]shared.JobID {
	errIDs := make([]shared.JobID, 0, len(r.Errors))
	for _, e := range r.Errors {
		errIDs = append(errIDs, e.Job)
	}
	out := [][]shared.JobID{r.Requeued, r.Completed, r.Failed, r.Unregistered, r.Corrupt, r.Conflicts, errIDs}
	for i := range out {
		out[i] = slices.Sorted(slices.Values(out[i]))
	}
	return out
}
