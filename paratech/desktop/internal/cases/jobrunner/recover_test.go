package jobrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

const (
	typeLegacy job.Type = "legacy"

	// payloadMarker stands for user data in a payload; it must never reach the log.
	payloadMarker = "PAYLOAD-MARKER"
)

// seedRunning stores a job as a crashed process leaves it: RUNNING with the given attempts used.
func (e *env) seedRunning(typ job.Type, attempts, maxAttempts int) shared.JobID {
	e.t.Helper()
	e.seq++
	id := shared.JobID(fmt.Sprintf("job-%d", e.seq))
	now := e.clock.Now()
	st := job.State{
		Version: 1, ID: id, Type: typ, Status: job.StatusRunning, Key: string(id),
		Payload:  fmt.Sprintf(`{"note":%q}`, payloadMarker),
		Attempts: attempts, MaxAttempts: maxAttempts, Checkpoint: "chunk-3", Percent: 40,
		NextRunAt: now, CreatedAt: now, StartedAt: now, UpdatedAt: now,
	}
	if _, err := job.Restore(st); err != nil {
		e.t.Fatalf("seeded job is not valid: %v", err)
	}
	e.repo.putRaw(st)
	return id
}

// registerRecovery registers a handler that must never run, with the given recovery policy.
func (e *env) registerRecovery(typ job.Type, policy Recovery, rc Reconciler) {
	e.t.Helper()
	h := handlerFunc(func(_ context.Context, snap job.Snapshot, _ func(context.Context, job.Progress) error) error {
		e.t.Errorf("handler of %s ran during recovery", snap.ID())
		return nil
	})
	if err := e.svc.Register(Registration{Type: typ, Handler: h, Recovery: policy, Reconciler: rc}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) recover() RecoveryReport {
	e.t.Helper()
	rep, err := e.svc.Recover(context.Background())
	if err != nil {
		e.t.Fatalf("Recover: %v", err)
	}
	return rep
}

// bucketOf names the report list that holds id and fails if it is in more or fewer than one.
func bucketOf(t *testing.T, rep RecoveryReport, id shared.JobID) string {
	t.Helper()
	var errIDs []shared.JobID
	for _, e := range rep.Errors {
		errIDs = append(errIDs, e.Job)
	}
	lists := []struct {
		name string
		ids  []shared.JobID
	}{
		{"requeued", rep.Requeued}, {"completed", rep.Completed}, {"failed", rep.Failed},
		{"unregistered", rep.Unregistered}, {"corrupt", rep.Corrupt}, {"conflicts", rep.Conflicts}, {"errors", errIDs},
	}
	var found []string
	for _, l := range lists {
		if slices.Contains(l.ids, id) {
			found = append(found, l.name)
		}
	}
	if len(found) != 1 {
		t.Fatalf("job %s is in %v of the report, want exactly one list", id, found)
	}
	return found[0]
}

func reconcileTo(next job.Status, reason string, err error) Reconciler {
	return reconcilerFunc(func(context.Context, job.Snapshot) (job.Status, string, error) { return next, reason, err })
}

func TestRecoverAppliesThePolicyOfTheType(t *testing.T) {
	errLookup := errors.New("mailbox unreachable")
	tests := []struct {
		name       string
		policy     Recovery
		rc         Reconciler
		attempts   int
		max        int
		wantStatus job.Status
		wantError  string // prefix of the stored last error
		wantBucket string
		wantErr    error // of the report entry
	}{
		{"requeue with attempts left", RecoveryRequeue, nil, 1, 3, job.StatusQueued, "", "requeued", nil},
		{"requeue without attempts left", RecoveryRequeue, nil, 3, 3, job.StatusFailed, "attempts exhausted", "failed", nil},
		{"fail policy", RecoveryFail, nil, 1, 3, job.StatusFailed, msgRecoveryFail, "failed", nil},
		{"side effect without a reconciler is not resent", RecoveryReconcile, nil, 1, 3, job.StatusFailed, msgNoReconciler, "failed", nil},
		{"reconciled as done", RecoveryReconcile, reconcileTo(job.StatusCompleted, "", nil), 1, 3, job.StatusCompleted, "", "completed", nil},
		{"reconciled as not sent", RecoveryReconcile, reconcileTo(job.StatusQueued, "", nil), 1, 3, job.StatusQueued, "", "requeued", nil},
		{"reconciled as not sent, no attempts left", RecoveryReconcile, reconcileTo(job.StatusQueued, "", nil), 2, 2, job.StatusFailed, "attempts exhausted", "failed", nil},
		{"reconciled as failed with a reason", RecoveryReconcile, reconcileTo(job.StatusFailed, "delivery outcome unknown", nil), 1, 3, job.StatusFailed, "delivery outcome unknown", "failed", nil},
		{"reconciled as failed without a reason", RecoveryReconcile, reconcileTo(job.StatusFailed, "", nil), 1, 3, job.StatusFailed, msgReconciledAsFails, "failed", nil},
		{"reconciler returns another status", RecoveryReconcile, reconcileTo(job.StatusRetryWait, "", nil), 1, 3, job.StatusRunning, "", "errors", ErrInvalidReconciliation},
		{"reconciler fails", RecoveryReconcile, reconcileTo("", "", errLookup), 1, 3, job.StatusRunning, "", "errors", errLookup},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.registerRecovery(typeMail, tc.policy, tc.rc)
			id := e.seedRunning(typeMail, tc.attempts, tc.max)

			rep := e.recover()

			st := e.repo.snapshot(id)
			if st.Status != tc.wantStatus || !strings.HasPrefix(st.LastError, tc.wantError) {
				t.Fatalf("job: %s %q, want %s %q", st.Status, st.LastError, tc.wantStatus, tc.wantError)
			}
			if st.Attempts != tc.attempts {
				t.Fatalf("attempts = %d, want %d: the crashed attempt counts and nothing else does", st.Attempts, tc.attempts)
			}
			if got := bucketOf(t, rep, id); got != tc.wantBucket {
				t.Fatalf("report list = %s, want %s", got, tc.wantBucket)
			}
			if tc.wantErr != nil && !errors.Is(rep.Errors[0].Err, tc.wantErr) {
				t.Fatalf("report error = %v, want %v", rep.Errors[0].Err, tc.wantErr)
			}
			if tc.wantStatus == job.StatusRunning && st.Version != 1 {
				t.Fatalf("a job left RUNNING was written (version %d)", st.Version)
			}
		})
	}
}

func TestReconcilerSeesTheStaleJob(t *testing.T) {
	e := newEnv(t, nil, nil)
	var seen atomic.Value
	e.registerRecovery(typeMail, RecoveryReconcile, reconcilerFunc(func(_ context.Context, snap job.Snapshot) (job.Status, string, error) {
		seen.Store(snap)
		return job.StatusCompleted, "", nil
	}))
	id := e.seedRunning(typeMail, 2, 3)
	e.recover()

	snap, _ := seen.Load().(job.Snapshot)
	if snap.ID() != id || snap.Status() != job.StatusRunning || snap.Attempt() != 2 || snap.Checkpoint() != "chunk-3" {
		t.Fatalf("reconciler saw %+v", snap)
	}
}

func TestReconcilerPanicLeavesTheJobRunning(t *testing.T) {
	e := newEnv(t, nil, nil)
	var logs bytes.Buffer
	e.svc.log = slog.New(slog.NewTextHandler(&logs, nil))
	e.registerRecovery(typeMail, RecoveryReconcile, reconcilerFunc(func(context.Context, job.Snapshot) (job.Status, string, error) {
		panic("reconciler bug")
	}))
	e.registerRecovery(typeStt, RecoveryRequeue, nil)
	broken := e.seedRunning(typeMail, 1, 3)
	fine := e.seedRunning(typeStt, 1, 3)

	rep := e.recover()

	if got := e.status(broken); got != job.StatusRunning || bucketOf(t, rep, broken) != "errors" {
		t.Fatalf("job of the panicking reconciler: %s in %s", got, bucketOf(t, rep, broken))
	}
	if got := e.status(fine); got != job.StatusQueued {
		t.Fatalf("the next job is %s, want QUEUED: one panic must not stop the recovery", got)
	}
	if !strings.Contains(logs.String(), "reconciler panicked") {
		t.Fatalf("panic not logged:\n%s", logs.String())
	}
}

// A hung reconciler must not block the app start: each call is bounded and a timeout leaves the job RUNNING.
func TestReconcilerIsBounded(t *testing.T) {
	tests := []struct {
		name       string
		answer     func(ctx context.Context) (job.Status, string, error)
		wantStatus job.Status
		wantBucket string
	}{
		{"hung reconciler times out", func(ctx context.Context) (job.Status, string, error) {
			return "", "", ctx.Err()
		}, job.StatusRunning, "errors"},
		{"an answer given after the bound is kept", func(context.Context) (job.Status, string, error) {
			return job.StatusFailed, "", nil
		}, job.StatusFailed, "failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.registerRecovery(typeMail, RecoveryReconcile, reconcilerFunc(func(ctx context.Context, _ job.Snapshot) (job.Status, string, error) {
				<-ctx.Done()
				return tc.answer(ctx)
			}))
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
			hung := e.seedRunning(typeMail, 1, 3)
			next := e.seedRunning(typeStt, 1, 3)

			type result struct {
				rep RecoveryReport
				err error
			}
			done := make(chan result, 1)
			go func() {
				rep, err := e.svc.Recover(context.Background())
				done <- result{rep, err}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := e.clock.AwaitSleepers(ctx, 1); err != nil {
				t.Fatalf("the reconciler bound did not start: %v", err)
			}
			e.clock.Advance(reconcileTimeout)

			var res result
			select {
			case res = <-done:
			case <-ctx.Done():
				t.Fatal("Recover did not return after the reconciler bound")
			}
			if res.err != nil {
				t.Fatal(res.err)
			}
			if got := e.status(hung); got != tc.wantStatus || bucketOf(t, res.rep, hung) != tc.wantBucket {
				t.Fatalf("hung job: %s in %s, want %s in %s", got, bucketOf(t, res.rep, hung), tc.wantStatus, tc.wantBucket)
			}
			if tc.wantBucket == "errors" && !errors.Is(res.rep.Errors[0].Err, ErrReconcileTimeout) {
				t.Fatalf("report error = %v, want ErrReconcileTimeout", res.rep.Errors[0].Err)
			}
			if got := e.status(next); got != job.StatusQueued {
				t.Fatalf("the next job is %s, want QUEUED", got)
			}
			e.start()
		})
	}
}

// A RUNNING job without a run in this runner is moved only by Recover, never cancelled behind its back.
func TestCancelRefusesAStaleRunningJob(t *testing.T) {
	tests := []struct {
		name     string
		typ      job.Type // of the stale job
		register job.Type
		policy   Recovery
		started  bool
	}{
		{"requeue type before start", typeStt, typeStt, RecoveryRequeue, false},
		{"reconcile type before start", typeMail, typeMail, RecoveryReconcile, false},
		{"unregistered type while running", typeLegacy, typeStt, RecoveryRequeue, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.registerRecovery(tc.register, tc.policy, nil)
			id := e.seedRunning(tc.typ, 1, 3)
			if tc.started {
				e.start()
			}

			if err := e.svc.Cancel(context.Background(), id); !errors.Is(err, ErrNotCancellable) {
				t.Fatalf("Cancel = %v, want ErrNotCancellable", err)
			}
			if st := e.repo.snapshot(id); st.Status != job.StatusRunning || st.Version != 1 {
				t.Fatalf("stale job was written: %s version %d", st.Status, st.Version)
			}
		})
	}
}

func TestRecoverHandlesEveryJobIndependently(t *testing.T) {
	errDisk := errors.New("disk full")
	var conflicted, saveFails shared.JobID
	var once atomic.Bool
	e := newEnvOn(t, nil, nil, func(m *memRepo) JobRepository {
		return &hookRepo{memRepo: m, beforeSave: func(j *job.Job) error {
			switch j.ID() {
			case saveFails:
				return errDisk
			case conflicted:
				if once.CompareAndSwap(false, true) { // another writer cancels the job first
					other, err := m.Get(context.Background(), j.ID())
					if err != nil {
						return err
					}
					if err := other.Cancel(t0); err != nil {
						return err
					}
					return m.Save(context.Background(), other)
				}
			}
			return nil
		}}
	})
	var logs bytes.Buffer
	e.svc.log = slog.New(slog.NewTextHandler(&logs, nil))
	e.registerRecovery(typeStt, RecoveryRequeue, nil)
	e.registerRecovery(typeMail, RecoveryReconcile, nil)

	requeued := e.seedRunning(typeStt, 1, 3)
	exhausted := e.seedRunning(typeStt, 3, 3)
	mail := e.seedRunning(typeMail, 1, 3)
	unknown := e.seedRunning(typeLegacy, 1, 3)
	conflicted = e.seedRunning(typeStt, 1, 3)
	saveFails = e.seedRunning(typeStt, 1, 3)
	corrupt := e.seedRunning(typeStt, 1, 3)
	bad := e.repo.snapshot(corrupt)
	bad.StartedAt = time.Time{} // a RUNNING job without a start time cannot be restored
	e.repo.putRaw(bad)
	queued := e.enqueue(typeStt, 3)

	rep := e.recover()

	want := map[shared.JobID]struct {
		bucket string
		status job.Status
	}{
		requeued:   {"requeued", job.StatusQueued},
		exhausted:  {"failed", job.StatusFailed},
		mail:       {"failed", job.StatusFailed},
		unknown:    {"unregistered", job.StatusRunning},
		conflicted: {"conflicts", job.StatusCancelled},
		saveFails:  {"errors", job.StatusRunning},
		corrupt:    {"corrupt", job.StatusRunning},
	}
	for id, w := range want {
		if got := bucketOf(t, rep, id); got != w.bucket {
			t.Errorf("%s: report list %s, want %s", id, got, w.bucket)
		}
		if got := e.status(id); got != w.status {
			t.Errorf("%s: status %s, want %s", id, got, w.status)
		}
	}
	if got := e.status(queued); got != job.StatusQueued || len(e.repo.path(queued)) != 1 {
		t.Errorf("a QUEUED job was touched: %s", got)
	}
	if !errors.Is(rep.Errors[0].Err, errDisk) || rep.Errors[0].Type != typeStt {
		t.Errorf("report error = %+v", rep.Errors[0])
	}
	if strings.Contains(logs.String(), payloadMarker) {
		t.Errorf("the log contains payload data:\n%s", logs.String())
	}
	for _, id := range []shared.JobID{unknown, corrupt, saveFails} {
		if !strings.Contains(logs.String(), string(id)) {
			t.Errorf("the log does not name skipped job %s", id)
		}
	}
}

func TestRecoverTwiceIsANoOp(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.registerRecovery(typeStt, RecoveryRequeue, nil)
	e.registerRecovery(typeMail, RecoveryReconcile, reconcileTo(job.StatusCompleted, "", nil))
	ids := []shared.JobID{e.seedRunning(typeStt, 1, 3), e.seedRunning(typeMail, 1, 3), e.seedRunning(typeLegacy, 1, 3)}
	first := e.recover()
	before := map[shared.JobID]job.State{}
	for _, id := range ids {
		before[id] = e.repo.snapshot(id)
	}

	second := e.recover()

	if len(second.Requeued)+len(second.Completed)+len(second.Failed)+len(second.Conflicts)+len(second.Errors) != 0 {
		t.Fatalf("second Recover changed jobs: %+v", second)
	}
	if fmt.Sprint(second.Unregistered) != fmt.Sprint(first.Unregistered) {
		t.Fatalf("unregistered jobs: first %v, second %v", first.Unregistered, second.Unregistered)
	}
	for _, id := range ids {
		if got := e.repo.snapshot(id); got != before[id] {
			t.Fatalf("%s changed by the second Recover:\n %+v\n %+v", id, before[id], got)
		}
	}
}

func TestRecoverLifecycle(t *testing.T) {
	errList := errors.New("database is locked")

	t.Run("after start", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.registerRecovery(typeStt, RecoveryRequeue, nil)
		e.start()
		if _, err := e.svc.Recover(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
			t.Fatalf("Recover = %v, want ErrAlreadyStarted", err)
		}
	})
	t.Run("after stop", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		if err := e.svc.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Recover(context.Background()); !errors.Is(err, ErrStopped) {
			t.Fatalf("Recover = %v, want ErrStopped", err)
		}
	})
	t.Run("listing fails", func(t *testing.T) {
		e := newEnvOn(t, nil, nil, func(m *memRepo) JobRepository { return &hookRepo{memRepo: m, listErr: errList} })
		if _, err := e.svc.Recover(context.Background()); !errors.Is(err, errList) {
			t.Fatalf("Recover = %v, want the listing error", err)
		}
	})
	t.Run("ended context", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.registerRecovery(typeStt, RecoveryRequeue, nil)
		id := e.seedRunning(typeStt, 1, 3)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := e.svc.Recover(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Recover = %v, want the context error", err)
		}
		if got := e.status(id); got != job.StatusRunning {
			t.Fatalf("status = %s, want RUNNING", got)
		}
	})
	t.Run("start waits for a running recovery", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		entered, release := make(chan struct{}), make(chan struct{})
		e.registerRecovery(typeMail, RecoveryReconcile, reconcilerFunc(func(context.Context, job.Snapshot) (job.Status, string, error) {
			close(entered)
			<-release
			return job.StatusFailed, "", nil
		}))
		id := e.seedRunning(typeMail, 1, 3)
		recovered := make(chan error, 1)
		go func() {
			_, err := e.svc.Recover(context.Background())
			recovered <- err
		}()
		<-entered

		started := make(chan error, 1)
		go func() { started <- e.svc.Start(context.Background()) }()
		select {
		case err := <-started:
			t.Fatalf("Start returned (%v) while Recover was running", err)
		case <-time.After(30 * time.Millisecond):
		}
		close(release)
		if err := <-recovered; err != nil {
			t.Fatal(err)
		}
		if err := <-started; err != nil {
			t.Fatal(err)
		}
		if got := e.status(id); got != job.StatusFailed {
			t.Fatalf("status = %s, want FAILED", got)
		}
	})
}

// A shutdown is handled like a crash for types that are not safe to requeue: the job stays RUNNING and the next
// start reconciles it (hard rule 7).
func TestShutdownLeavesUnsafeJobsForRecovery(t *testing.T) {
	tests := []struct {
		name       string
		policy     Recovery
		rc         Reconciler
		afterStop  job.Status
		afterStart job.Status
	}{
		{"requeue", RecoveryRequeue, nil, job.StatusQueued, job.StatusQueued},
		{"reconcile", RecoveryReconcile, reconcileTo(job.StatusCompleted, "", nil), job.StatusRunning, job.StatusCompleted},
		{"reconcile without a reconciler", RecoveryReconcile, nil, job.StatusRunning, job.StatusFailed},
		{"fail", RecoveryFail, nil, job.StatusRunning, job.StatusFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			var calls atomic.Int32
			started := make(chan struct{})
			err := e.svc.Register(Registration{Type: typeMail, Recovery: tc.policy, Reconciler: tc.rc, Handler: handlerFunc(
				func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
					if calls.Add(1) == 1 {
						close(started)
					}
					<-ctx.Done()
					return ctx.Err()
				})})
			if err != nil {
				t.Fatal(err)
			}
			id := e.enqueue(typeMail, 3)
			e.start()
			<-started

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := e.svc.Stop(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("Stop = %v", err)
			}
			if st := e.repo.snapshot(id); st.Status != tc.afterStop || st.Attempts != 1 {
				t.Fatalf("after the shutdown: %s attempts=%d, want %s", st.Status, st.Attempts, tc.afterStop)
			}
			if tc.afterStop == job.StatusRunning {
				if err := e.svc.Cancel(context.Background(), id); !errors.Is(err, ErrNotCancellable) {
					t.Fatalf("Cancel of a job left RUNNING by Stop = %v, want ErrNotCancellable", err)
				}
			}

			// The next process on the same store.
			next, err := New(Deps{Jobs: e.repo, Clock: e.clock, Sleeper: e.clock}, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = next.Stop(context.Background()) }()
			if err := next.Register(Registration{Type: typeMail, Recovery: tc.policy, Reconciler: tc.rc, Handler: handlerFunc(
				func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })}); err != nil {
				t.Fatal(err)
			}
			if _, err := next.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := e.status(id); got != tc.afterStart {
				t.Fatalf("after recovery: %s, want %s", got, tc.afterStart)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("handler calls = %d, want 1", got)
			}
		})
	}
}
