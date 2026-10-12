package tests

// Scenario tests of the job runner on the real SQLite adapter (TASK-009): only the clock is fake (ARCH-61).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var runnerT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

const runnerType job.Type = "stt"

type progressFn = func(context.Context, job.Progress) error

type runnerEnv struct {
	t     *testing.T
	db    *sqlite.DB
	repo  *sqlite.JobRepository
	clock *testenv.FakeClock
	svc   *jobrunner.Service
	seq   int

	closed bool
}

func runnerConfig(workers int) jobrunner.Config {
	return jobrunner.Config{
		Workers:        workers,
		PollInterval:   time.Second,
		DefaultTimeout: 10 * time.Minute,
		Backoff:        backoff.Spec{Base: 5 * time.Second, Factor: 2, MaxDelay: time.Minute, MaxAttempts: 10},
	}
}

func openRunnerDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), sqlite.Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

// newRunnerEnv opens a database at path and builds a stopped Service. Close it with env.shutdown.
func newRunnerEnv(t *testing.T, path string, clock *testenv.FakeClock, workers int, h jobrunner.Handler) *runnerEnv {
	t.Helper()
	db := openRunnerDB(t, path)
	repo := sqlite.NewJobRepository(db)
	svc, err := jobrunner.New(jobrunner.Deps{Jobs: repo, Clock: clock, Sleeper: clock}, runnerConfig(workers))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Register(jobrunner.Registration{Type: runnerType, Handler: h}); err != nil {
		t.Fatal(err)
	}
	e := &runnerEnv{t: t, db: db, repo: repo, clock: clock, svc: svc}
	t.Cleanup(e.shutdown)
	return e
}

// shutdown stops the service and closes the database; the environment is unusable afterwards.
func (e *runnerEnv) shutdown() {
	e.t.Helper()
	if e.closed {
		return
	}
	e.closed = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.svc.Stop(ctx); err != nil {
		e.t.Errorf("Stop: %v", err)
	}
	if err := e.db.Close(); err != nil {
		e.t.Errorf("Close: %v", err)
	}
}

func (e *runnerEnv) enqueue(maxAttempts int) shared.JobID {
	return e.enqueueAt(maxAttempts, time.Time{})
}

// enqueueAt stores a job that becomes eligible at runAt (zero means now).
func (e *runnerEnv) enqueueAt(maxAttempts int, runAt time.Time) shared.JobID {
	e.t.Helper()
	e.seq++
	id := shared.JobID(fmt.Sprintf("job-%d", e.seq))
	j, err := job.New(job.Spec{ID: id, Type: runnerType, Key: string(id), MaxAttempts: maxAttempts, RunAt: runAt}, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, created, err := e.repo.Enqueue(context.Background(), j); err != nil || !created {
		e.t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
	return id
}

func (e *runnerEnv) get(id shared.JobID) *job.Job {
	e.t.Helper()
	j, err := e.repo.Get(context.Background(), id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	return j
}

func (e *runnerEnv) status(id shared.JobID) job.Status { return e.get(id).Status() }

func runnerEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	testenv.Eventually(t, what, cond, nil)
}

// drive advances the fake clock in poll-sized steps until cond holds.
func (e *runnerEnv) drive(what string, cond func() bool) {
	e.t.Helper()
	testenv.Eventually(e.t, what, cond, func() { e.clock.Advance(time.Second) })
}

func (e *runnerEnv) start() {
	e.t.Helper()
	if err := e.svc.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func runnerDB(t *testing.T) string { return filepath.Join(t.TempDir(), "jobs.db") }

func TestJobRunnerRetriesThenCompletes(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(runnerT0)
	var calls atomic.Int32
	e := newRunnerEnv(t, runnerDB(t), clock, 2, handlerFn(func(ctx context.Context, _ job.Snapshot, _ progressFn) error {
		if n := calls.Add(1); n <= 3 {
			return fmt.Errorf("transient failure %d", n)
		}
		return nil
	}))
	id := e.enqueue(5)
	e.start()

	var delays []time.Duration
	var attempts []int
	for n := int32(1); n <= 3; n++ {
		runnerEventually(t, fmt.Sprintf("RETRY_WAIT after call %d", n), func() bool {
			return calls.Load() == n && e.status(id) == job.StatusRetryWait
		})
		j := e.get(id)
		delays = append(delays, j.NextRunAt().Sub(j.UpdatedAt()))
		attempts = append(attempts, j.Attempts())
		if !strings.Contains(j.LastError(), fmt.Sprintf("transient failure %d", n)) {
			t.Errorf("lastError after call %d = %q", n, j.LastError())
		}
		e.drive("the next call", func() bool { return calls.Load() > n })
	}
	runnerEventually(t, "COMPLETED", func() bool { return e.status(id) == job.StatusCompleted })

	if want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}; fmt.Sprint(delays) != fmt.Sprint(want) {
		t.Fatalf("backoff delays = %v, want %v", delays, want)
	}
	if fmt.Sprint(attempts) != "[1 2 3]" {
		t.Fatalf("attempts at each retry = %v", attempts)
	}
	if j := e.get(id); j.Attempts() != 4 || j.Progress().Percent() != 100 || j.LastError() != "" {
		t.Fatalf("completed job: attempts=%d progress=%d lastError=%q", j.Attempts(), j.Progress().Percent(), j.LastError())
	}
	e.shutdown()
}

func TestJobRunnerFailsAfterTheAttemptCap(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(runnerT0)
	var calls atomic.Int32
	e := newRunnerEnv(t, runnerDB(t), clock, 2, handlerFn(func(context.Context, job.Snapshot, progressFn) error {
		calls.Add(1)
		return errors.New("always broken")
	}))
	id := e.enqueue(3)
	e.start()
	e.drive("FAILED", func() bool { return e.status(id) == job.StatusFailed })
	for range 100 { // a failed job is never picked up again
		clock.Advance(time.Minute)
	}
	time.Sleep(30 * time.Millisecond)

	if got := calls.Load(); got != 3 {
		t.Fatalf("handler calls = %d, want 3 (AC-10: bounded retries)", got)
	}
	if j := e.get(id); j.Status() != job.StatusFailed || j.Attempts() != 3 || j.FinishedAt().IsZero() {
		t.Fatalf("final: %s attempts=%d finished=%v", j.Status(), j.Attempts(), j.FinishedAt())
	}
	e.shutdown()
}

func TestJobRunnerCancelsARunningHandler(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(runnerT0)
	started := make(chan struct{})
	var sawDone atomic.Bool
	e := newRunnerEnv(t, runnerDB(t), clock, 2, handlerFn(func(ctx context.Context, _ job.Snapshot, report progressFn) error {
		p, _ := job.NewProgress(25, "chunk-1")
		if err := report(ctx, p); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		sawDone.Store(true)
		return ctx.Err()
	}))
	id := e.enqueue(3)
	e.start()
	<-started

	if got := e.get(id); got.Status() != job.StatusRunning || got.Progress().Percent() != 25 {
		t.Fatalf("running job in the database: %s %d%%", got.Status(), got.Progress().Percent())
	}
	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	runnerEventually(t, "CANCELLED", func() bool { return e.status(id) == job.StatusCancelled })
	if !sawDone.Load() {
		t.Fatal("the handler never saw ctx.Done()")
	}
	if got := e.get(id); got.Progress().Percent() != 25 || got.FinishedAt().IsZero() {
		t.Fatalf("cancelled job: progress=%d finished=%v", got.Progress().Percent(), got.FinishedAt())
	}
	e.shutdown()
}

func TestJobRunnerCancelsAQueuedJobDirectly(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(runnerT0)
	e := newRunnerEnv(t, runnerDB(t), clock, 1, handlerFn(func(context.Context, job.Snapshot, progressFn) error {
		t.Error("a cancelled job ran")
		return nil
	}))
	id := e.enqueue(3)
	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.start()
	for range 50 {
		clock.Advance(time.Second)
	}
	time.Sleep(30 * time.Millisecond)
	if got := e.status(id); got != job.StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", got)
	}
	e.shutdown()
}

func TestJobRunnerQueueSurvivesARestart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	path := runnerDB(t)
	clock := testenv.NewFakeClock(runnerT0)

	// First process: one job fails after saving progress; another is queued for later and never runs.
	first := newRunnerEnv(t, path, clock, 1, handlerFn(func(ctx context.Context, snap job.Snapshot, report progressFn) error {
		if snap.ID() != "job-1" {
			t.Errorf("%s must not run in the first process", snap.ID())
		}
		p, _ := job.NewProgress(30, "chunk-2")
		if err := report(ctx, p); err != nil {
			return err
		}
		return errors.New("failure before the restart")
	}))
	retrying := first.enqueue(5)
	later := first.enqueueAt(3, runnerT0.Add(time.Hour))
	first.start()
	runnerEventually(t, "RETRY_WAIT", func() bool { return first.status(retrying) == job.StatusRetryWait })
	first.shutdown()

	// Second process on the same file.
	var ran sync.Map
	second := newRunnerEnv(t, path, clock, 2, handlerFn(func(_ context.Context, snap job.Snapshot, _ progressFn) error {
		ran.Store(snap.ID(), fmt.Sprintf("attempt=%d checkpoint=%s", snap.Attempt(), snap.Checkpoint()))
		return nil
	}))
	if j := second.get(retrying); j.Status() != job.StatusRetryWait || j.Attempts() != 1 || j.Progress().Checkpoint() != "chunk-2" {
		t.Fatalf("after restart: %s attempts=%d checkpoint=%q", j.Status(), j.Attempts(), j.Progress().Checkpoint())
	}
	if got := second.status(later); got != job.StatusQueued {
		t.Fatalf("queued job after restart is %s, want QUEUED", got)
	}
	second.start()
	clock.Advance(time.Hour) // the backoff and the delayed job are both due now
	second.drive("both jobs complete", func() bool {
		return second.status(retrying) == job.StatusCompleted && second.status(later) == job.StatusCompleted
	})
	if got, _ := ran.Load(retrying); got != "attempt=2 checkpoint=chunk-2" {
		t.Fatalf("retried job saw %q, want its second attempt with the saved checkpoint", got)
	}
	if got, _ := ran.Load(later); got != "attempt=1 checkpoint=" {
		t.Fatalf("delayed job saw %q", got)
	}
	second.shutdown()
}

func TestJobRunnerWorkersNeverShareAJob(t *testing.T) {
	const jobs = 30
	testenv.NoGoroutineLeaks(t)
	clock := testenv.NewFakeClock(runnerT0)
	var mu sync.Mutex
	active := map[shared.JobID]int{}
	runs := map[shared.JobID]int{}
	var overlap atomic.Bool
	e := newRunnerEnv(t, runnerDB(t), clock, 4, handlerFn(func(_ context.Context, snap job.Snapshot, _ progressFn) error {
		mu.Lock()
		active[snap.ID()]++
		runs[snap.ID()]++
		if active[snap.ID()] > 1 {
			overlap.Store(true)
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active[snap.ID()]--
		mu.Unlock()
		return nil
	}))
	ids := make([]shared.JobID, jobs)
	for i := range ids {
		ids[i] = e.enqueue(2)
	}
	e.start()
	e.drive("every job completes", func() bool {
		for _, id := range ids {
			if e.status(id) != job.StatusCompleted {
				return false
			}
		}
		return true
	})
	mu.Lock()
	for _, id := range ids {
		if runs[id] != 1 {
			t.Errorf("job %s ran %d times, want 1", id, runs[id])
		}
	}
	mu.Unlock()
	if overlap.Load() {
		t.Error("a job ran on two workers at once")
	}
	e.shutdown()
}

// handlerFn adapts a function to jobrunner.Handler.
type handlerFn func(ctx context.Context, snap job.Snapshot, report progressFn) error

func (f handlerFn) Handle(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
	return f(ctx, snap, report)
}
