package jobrunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

const (
	typeStt  job.Type = "stt"
	typeMail job.Type = "mail"
)

func testConfig() Config {
	return Config{
		Workers:        1,
		PollInterval:   time.Second,
		DefaultTimeout: 10 * time.Minute,
		Backoff:        backoff.Spec{Base: 5 * time.Second, Factor: 2, MaxDelay: time.Minute, MaxAttempts: 5},
	}
}

// env is a Service on an in-memory repository and a fake clock.
type env struct {
	t     *testing.T
	repo  *memRepo
	clock *testenv.FakeClock
	svc   *Service
	seq   int
}

func newEnv(t *testing.T, mutate func(*Config), jitter Jitter) *env {
	t.Helper()
	return newEnvOn(t, mutate, jitter, nil)
}

// newEnvOn is newEnv with the runner reaching the repository through wrap, for tests that inject failures.
func newEnvOn(t *testing.T, mutate func(*Config), jitter Jitter, wrap func(*memRepo) JobRepository) *env {
	t.Helper()
	testenv.NoGoroutineLeaks(t)
	cfg := testConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	e := &env{t: t, repo: newMemRepo(), clock: testenv.NewFakeClock(t0)}
	var jobs JobRepository = e.repo
	if wrap != nil {
		jobs = wrap(e.repo)
	}
	svc, err := New(Deps{Jobs: jobs, Clock: e.clock, Sleeper: e.clock, Jitter: jitter}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return e
}

func (e *env) register(typ job.Type, h handlerFunc) {
	e.t.Helper()
	if err := e.svc.Register(Registration{Type: typ, Handler: h, Recovery: RecoveryRequeue}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) start() {
	e.t.Helper()
	if err := e.svc.Start(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) enqueue(typ job.Type, maxAttempts int) shared.JobID {
	e.t.Helper()
	e.seq++
	id := shared.JobID(fmt.Sprintf("job-%d", e.seq))
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: string(id), MaxAttempts: maxAttempts}, e.clock.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	if _, created, err := e.repo.Enqueue(context.Background(), j); err != nil || !created {
		e.t.Fatalf("Enqueue: created=%v err=%v", created, err)
	}
	return id
}

func (e *env) status(id shared.JobID) job.Status { return e.repo.snapshot(id).Status }

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	testenv.Eventually(t, what, cond, nil)
}

func (e *env) waitStatus(id shared.JobID, want job.Status) {
	e.t.Helper()
	eventually(e.t, fmt.Sprintf("%s to be %s", id, want), func() bool { return e.status(id) == want })
}

// drive moves the fake clock in poll-sized steps until cond holds, so waits and retries come due.
func (e *env) drive(what string, cond func() bool) {
	e.t.Helper()
	testenv.Eventually(e.t, what, cond, func() { e.clock.Advance(time.Second) })
}

func (e *env) idle(workers int) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.clock.AwaitSleepers(ctx, workers+1); err != nil { // the workers and the promoter
		e.t.Fatalf("workers did not go idle: %v", err)
	}
}

func statuses(path []stored) []job.Status {
	out := make([]job.Status, len(path))
	for i, s := range path {
		out[i] = s.Status
	}
	return out
}

func equalStatuses(a, b []job.Status) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNewValidatesConfig(t *testing.T) {
	repo, clock := newMemRepo(), testenv.NewFakeClock(t0)
	good := Deps{Jobs: repo, Clock: clock, Sleeper: clock}
	tests := []struct {
		name   string
		deps   Deps
		mutate func(*Config)
		ok     bool
	}{
		{"defaults", good, func(c *Config) { *c = DefaultConfig() }, true},
		{"test config", good, nil, true},
		{"no repository", Deps{Clock: clock, Sleeper: clock}, nil, false},
		{"no clock", Deps{Jobs: repo, Sleeper: clock}, nil, false},
		{"no sleeper", Deps{Jobs: repo, Clock: clock}, nil, false},
		{"no workers", good, func(c *Config) { c.Workers = 0 }, false},
		{"no poll interval", good, func(c *Config) { c.PollInterval = 0 }, false},
		{"no timeout", good, func(c *Config) { c.DefaultTimeout = 0 }, false},
		{"bad backoff", good, func(c *Config) { c.Backoff.MaxAttempts = 0 }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			_, err := New(tc.deps, cfg)
			if (err == nil) != tc.ok {
				t.Fatalf("New = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestRegisterValidation(t *testing.T) {
	noop := handlerFunc(func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	noReconcile := reconcilerFunc(func(context.Context, job.Snapshot) (job.Status, string, error) { return job.StatusFailed, "", nil })
	tests := []struct {
		name string
		reg  Registration
	}{
		{"nil handler", Registration{Type: typeStt}},
		{"blank type", Registration{Type: "", Handler: noop, Recovery: RecoveryRequeue}},
		{"type with space", Registration{Type: "a b", Handler: noop, Recovery: RecoveryRequeue}},
		{"negative timeout", Registration{Type: typeStt, Handler: noop, Timeout: -time.Second, Recovery: RecoveryRequeue}},
		{"no recovery policy", Registration{Type: typeStt, Handler: noop}},
		{"unknown recovery policy", Registration{Type: typeStt, Handler: noop, Recovery: RecoveryFail + 1}},
		{"reconciler without the reconcile policy", Registration{Type: typeStt, Handler: noop, Recovery: RecoveryRequeue, Reconciler: noReconcile}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			if err := e.svc.Register(tc.reg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Register = %v, want ErrInvalidConfig", err)
			}
		})
	}

	t.Run("duplicate type", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.register(typeStt, noop)
		if err := e.svc.Register(Registration{Type: typeStt, Handler: noop, Recovery: RecoveryRequeue}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("second Register = %v, want ErrInvalidConfig", err)
		}
	})
}

func TestLifecycleErrors(t *testing.T) {
	noop := handlerFunc(func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })

	t.Run("start without handlers", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		if err := e.svc.Start(context.Background()); !errors.Is(err, ErrNoHandlers) {
			t.Fatalf("Start = %v, want ErrNoHandlers", err)
		}
	})
	t.Run("start twice and register after start", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.register(typeStt, noop)
		e.start()
		if err := e.svc.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
			t.Fatalf("second Start = %v, want ErrAlreadyStarted", err)
		}
		if err := e.svc.Register(Registration{Type: typeMail, Handler: noop, Recovery: RecoveryRequeue}); !errors.Is(err, ErrAlreadyStarted) {
			t.Fatalf("Register after Start = %v, want ErrAlreadyStarted", err)
		}
	})
	t.Run("start after stop", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.register(typeStt, noop)
		if err := e.svc.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Start(context.Background()); !errors.Is(err, ErrStopped) {
			t.Fatalf("Start after Stop = %v, want ErrStopped", err)
		}
	})
	t.Run("stop twice", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.register(typeStt, noop)
		e.start()
		for range 2 {
			if err := e.svc.Stop(context.Background()); err != nil {
				t.Fatalf("Stop = %v", err)
			}
		}
	})
}

func TestSuccessCompletesAndPersistsProgress(t *testing.T) {
	e := newEnv(t, nil, nil)
	reported := make(chan struct{})
	release := make(chan struct{})
	e.register(typeStt, func(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
		p, err := job.NewProgress(40, "chunk-3")
		if err != nil {
			return err
		}
		if err := report(ctx, p); err != nil {
			return err
		}
		close(reported)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	id := e.enqueue(typeStt, 3)
	e.start()

	<-reported
	st := e.repo.snapshot(id)
	if st.Status != job.StatusRunning || st.Percent != 40 || st.Checkpoint != "chunk-3" {
		t.Fatalf("while running: %s %d%% %q, want RUNNING 40%% chunk-3 stored in the repository", st.Status, st.Percent, st.Checkpoint)
	}

	close(release)
	e.waitStatus(id, job.StatusCompleted)
	if st := e.repo.snapshot(id); st.Percent != 100 || st.Attempts != 1 || st.FinishedAt.IsZero() {
		t.Fatalf("completed job: %d%% attempts=%d finished=%v", st.Percent, st.Attempts, st.FinishedAt)
	}
	want := []job.Status{job.StatusQueued, job.StatusRunning, job.StatusRunning, job.StatusCompleted}
	if got := statuses(e.repo.path(id)); !equalStatuses(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
}

func TestRetryWithGrowingBackoffThenSuccess(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	var resumed atomic.Value
	e.register(typeStt, func(ctx context.Context, snap job.Snapshot, report func(context.Context, job.Progress) error) error {
		n := calls.Add(1)
		if n == 2 {
			resumed.Store(snap.Checkpoint())
		}
		p, _ := job.NewProgress(int(n)*10, fmt.Sprintf("after-%d", n))
		if err := report(ctx, p); err != nil {
			return err
		}
		if n <= 3 {
			return fmt.Errorf("flaky network, call %d", n)
		}
		return nil
	})
	id := e.enqueue(typeStt, 5)
	e.start()
	e.drive("the job completes", func() bool { return e.status(id) == job.StatusCompleted })

	path := e.repo.path(id)
	var delays []time.Duration
	var visited []job.Status
	for _, s := range path {
		if s.Status == job.StatusRetryWait {
			delays = append(delays, s.Delay)
		}
		if n := len(visited); n == 0 || visited[n-1] != s.Status {
			visited = append(visited, s.Status)
		}
	}
	wantPath := []job.Status{
		job.StatusQueued, job.StatusRunning, job.StatusRetryWait, job.StatusQueued, job.StatusRunning, job.StatusRetryWait,
		job.StatusQueued, job.StatusRunning, job.StatusRetryWait, job.StatusQueued, job.StatusRunning, job.StatusCompleted,
	}
	if !equalStatuses(visited, wantPath) {
		t.Fatalf("path = %v\nwant   %v", visited, wantPath)
	}
	wantDelays := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	if fmt.Sprint(delays) != fmt.Sprint(wantDelays) {
		t.Fatalf("backoff delays = %v, want %v", delays, wantDelays)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("handler calls = %d, want 4", got)
	}
	if got := resumed.Load(); got != "after-1" {
		t.Fatalf("second attempt saw checkpoint %v, want after-1 (progress survives a retry)", got)
	}
	if st := e.repo.snapshot(id); st.Attempts != 4 || st.LastError != "" {
		t.Fatalf("final: attempts=%d lastError=%q", st.Attempts, st.LastError)
	}
}

func TestRetryNeverRunsBeforeTheBackoffElapsed(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		calls.Add(1)
		return errors.New("boom")
	})
	id := e.enqueue(typeStt, 5)
	e.start()
	e.waitStatus(id, job.StatusRetryWait)
	e.idle(1)

	e.clock.Advance(4 * time.Second) // below the 5s backoff: several polls pass, nothing runs
	e.idle(1)
	e.clock.Advance(500 * time.Millisecond)
	e.idle(1)
	if got, status := calls.Load(), e.status(id); got != 1 || status != job.StatusRetryWait {
		t.Fatalf("calls=%d status=%s before the backoff elapsed, want 1 and RETRY_WAIT", got, status)
	}

	e.drive("the second call", func() bool { return calls.Load() >= 2 })
}

func TestAttemptsAreBounded(t *testing.T) {
	tests := []struct {
		name        string
		jobAttempts int
		policyMax   int
		wantCalls   int32
	}{
		{"job cap is lower", 2, 10, 2},
		{"policy cap is lower", 10, 3, 3},
		{"both equal", 4, 4, 4},
		{"single attempt", 1, 5, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(c *Config) { c.Backoff.MaxAttempts = tc.policyMax }, nil)
			var calls atomic.Int32
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				calls.Add(1)
				return errors.New("always broken")
			})
			id := e.enqueue(typeStt, tc.jobAttempts)
			e.start()
			e.drive("the job fails", func() bool { return e.status(id) == job.StatusFailed })

			// Keep the clock running: a FAILED job must never be picked up again.
			for range 200 {
				e.clock.Advance(time.Minute)
			}
			time.Sleep(20 * time.Millisecond)
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("handler calls = %d, want %d", got, tc.wantCalls)
			}
			st := e.repo.snapshot(id)
			if st.Status != job.StatusFailed || st.Attempts != int(tc.wantCalls) || !strings.Contains(st.LastError, "always broken") ||
				!strings.HasPrefix(st.LastError, "attempts exhausted: ") {
				t.Fatalf("final: %s attempts=%d lastError=%q", st.Status, st.Attempts, st.LastError)
			}
		})
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		handle     func() error
		want       job.Status
		wantErrHas string
	}{
		{"permanent fails at once", func() error { return backoff.Permanent(errors.New("bad payload")) }, job.StatusFailed, "bad payload"},
		{"storage permission is permanent", func() error { return fmt.Errorf("write: %w", shared.ErrStoragePermission) }, job.StatusFailed, "permission"},
		{"transient retries", func() error { return backoff.Transient(errors.New("reset")) }, job.StatusRetryWait, "reset"},
		{"unclassified retries", func() error { return errors.New("who knows") }, job.StatusRetryWait, "who knows"},
		{"network error retries", func() error { return shared.ErrNetworkUnavailable }, job.StatusRetryWait, "network"},
		{"panic fails", func() error { panic("nil map") }, job.StatusFailed, "panic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				return tc.handle()
			})
			id := e.enqueue(typeStt, 5)
			e.start()
			e.waitStatus(id, tc.want)
			if st := e.repo.snapshot(id); st.Attempts != 1 || !strings.Contains(st.LastError, tc.wantErrHas) {
				t.Fatalf("attempts=%d lastError=%q, want 1 and %q", st.Attempts, st.LastError, tc.wantErrHas)
			}
		})
	}
}

func TestJitterShortensTheDelay(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Backoff.Jitter = 0.5 }, fixedJitter(500))
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		return errors.New("boom")
	})
	id := e.enqueue(typeStt, 5)
	e.start()
	e.waitStatus(id, job.StatusRetryWait)
	if got, want := e.repo.snapshot(id).NextRunAt.Sub(e.repo.snapshot(id).UpdatedAt), 3750*time.Millisecond; got != want {
		t.Fatalf("delay = %v, want %v (5s reduced by 50%% of 50%%)", got, want)
	}
}

func TestTimeoutInterruptsTheHandlerAndRetries(t *testing.T) {
	e := newEnv(t, nil, nil)
	if err := e.svc.Register(Registration{Type: typeStt, Timeout: 30 * time.Second, Recovery: RecoveryRequeue, Handler: handlerFunc(
		func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
			<-ctx.Done()
			return ctx.Err()
		})}); err != nil {
		t.Fatal(err)
	}
	id := e.enqueue(typeStt, 2)
	e.start()
	e.waitStatus(id, job.StatusRunning)
	e.idle(1) // the worker is not idle (it runs the job) but the promoter, the worker's timer and the job timer sleep
	e.clock.Advance(29 * time.Second)
	time.Sleep(10 * time.Millisecond)
	if got := e.status(id); got != job.StatusRunning {
		t.Fatalf("status = %s before the timeout, want RUNNING", got)
	}

	e.clock.Advance(time.Second)
	e.waitStatus(id, job.StatusRetryWait)
	if st := e.repo.snapshot(id); !strings.Contains(st.LastError, "timed out after 30s") {
		t.Fatalf("lastError = %q, want the timeout", st.LastError)
	}

	e.drive("the second attempt times out too", func() bool { return e.status(id) == job.StatusFailed })
}

func TestCancelRunningJobStopsTheHandler(t *testing.T) {
	e := newEnv(t, nil, nil)
	started := make(chan struct{})
	var seen atomic.Value
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-ctx.Done()
		seen.Store(context.Cause(ctx))
		return ctx.Err()
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	<-started

	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	e.waitStatus(id, job.StatusCancelled)
	if cause, _ := seen.Load().(error); !errors.Is(cause, errCancelled) {
		t.Fatalf("handler saw cause %v, want the user cancellation", cause)
	}
	if st := e.repo.snapshot(id); st.FinishedAt.IsZero() || st.Attempts != 1 {
		t.Fatalf("cancelled job: finished=%v attempts=%d", st.FinishedAt, st.Attempts)
	}
}

func TestCancelledJobDoesNotRetryOrRunAgain(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	started := make(chan struct{})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-ctx.Done()
		return errors.New("killed while cancelling") // an error that would be retried were it not a cancellation
	})
	id := e.enqueue(typeStt, 5)
	e.start()
	<-started
	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	e.waitStatus(id, job.StatusCancelled)
	for range 100 {
		e.clock.Advance(time.Minute)
	}
	time.Sleep(20 * time.Millisecond)
	if got := calls.Load(); got != 1 || e.status(id) != job.StatusCancelled {
		t.Fatalf("calls=%d status=%s, want 1 and CANCELLED", got, e.status(id))
	}
}

func TestCancelJobThatWaits(t *testing.T) {
	tests := []struct {
		name string
		prep func(e *env) shared.JobID
	}{
		{"queued, not yet due", func(e *env) shared.JobID {
			j, _ := job.New(job.Spec{ID: "later", Type: typeStt, Key: "later", MaxAttempts: 2, RunAt: t0.Add(time.Hour)}, t0)
			if _, _, err := e.repo.Enqueue(context.Background(), j); err != nil {
				t.Fatal(err)
			}
			return "later"
		}},
		{"queued, type without a handler", func(e *env) shared.JobID { return e.enqueue("other", 2) }},
		{"waiting for the retry", func(e *env) shared.JobID {
			id := e.enqueue(typeStt, 5)
			e.idle(1)
			e.clock.Advance(time.Second) // one poll: the worker claims the job and it fails
			e.waitStatus(id, job.StatusRetryWait)
			return id
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			var calls atomic.Int32
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				calls.Add(1)
				return errors.New("boom")
			})
			e.start()
			id := tc.prep(e)

			if err := e.svc.Cancel(context.Background(), id); err != nil {
				t.Fatalf("Cancel: %v", err)
			}
			if got := e.status(id); got != job.StatusCancelled {
				t.Fatalf("status = %s, want CANCELLED", got)
			}
			before := calls.Load()
			for range 100 {
				e.clock.Advance(time.Minute)
			}
			time.Sleep(20 * time.Millisecond)
			if got := calls.Load(); got != before {
				t.Fatalf("handler ran %d more times after cancellation", got-before)
			}
		})
	}
}

func TestCancelIsRefused(t *testing.T) {
	t.Run("finished job", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
		id := e.enqueue(typeStt, 2)
		e.start()
		e.waitStatus(id, job.StatusCompleted)
		if err := e.svc.Cancel(context.Background(), id); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("Cancel = %v, want ErrNotCancellable", err)
		}
	})
	t.Run("unknown job", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		if err := e.svc.Cancel(context.Background(), "nope"); !errors.Is(err, shared.ErrNotFound) {
			t.Fatalf("Cancel = %v, want ErrNotFound", err)
		}
	})
	t.Run("running job of an uncancellable type", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		started, release := make(chan struct{}), make(chan struct{})
		err := e.svc.Register(Registration{Type: typeMail, Uncancellable: true, Recovery: RecoveryReconcile, Handler: handlerFunc(
			func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
				close(started)
				<-release
				return ctx.Err() // still nil: the context was not cancelled
			})})
		if err != nil {
			t.Fatal(err)
		}
		id := e.enqueue(typeMail, 2)
		e.start()
		<-started
		if err := e.svc.Cancel(context.Background(), id); !errors.Is(err, ErrNotCancellable) {
			t.Fatalf("Cancel = %v, want ErrNotCancellable", err)
		}
		close(release)
		e.waitStatus(id, job.StatusCompleted)
	})
}

func TestCancelIsRefusedWhileTheFinishedRunIsStillRegistered(t *testing.T) {
	var once sync.Once
	stored, release := make(chan struct{}), make(chan struct{})
	e := newEnvOn(t, nil, nil, func(m *memRepo) JobRepository {
		return &hookRepo{memRepo: m, afterSave: func(j *job.Job) {
			if j.Status() != job.StatusCompleted {
				return
			}
			// The outcome is stored but the run has not left execute: it is still in the running map.
			once.Do(func() {
				close(stored)
				<-release
			})
		}}
	})
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	id := e.enqueue(typeStt, 2)
	e.start()
	<-stored

	err := e.svc.Cancel(context.Background(), id)
	close(release)
	if !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("Cancel = %v, want ErrNotCancellable", err)
	}
	if got := e.status(id); got != job.StatusCompleted {
		t.Fatalf("status = %s, want COMPLETED", got)
	}
}

func TestHandlerThatFinishesDespiteCancellationCompletes(t *testing.T) {
	e := newEnv(t, nil, nil)
	started, release := make(chan struct{}), make(chan struct{})
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		close(started)
		<-release // ignores the context on purpose
		return nil
	})
	id := e.enqueue(typeStt, 2)
	e.start()
	<-started
	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(release)
	e.waitStatus(id, job.StatusCompleted)
}

func TestJobChangedElsewhereStopsTheHandler(t *testing.T) {
	e := newEnv(t, nil, nil)
	started, proceed := make(chan struct{}), make(chan struct{})
	var reportErr, ctxErr atomic.Value
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		close(started)
		<-proceed
		err := report(ctx, job.Progress{})
		reportErr.Store(err)
		<-ctx.Done()
		ctxErr.Store(context.Cause(ctx))
		return err
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	<-started

	// Another writer (not the runner) cancels the job in the repository.
	j, err := e.repo.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Cancel(e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.Save(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	close(proceed)

	eventually(t, "the handler to see the conflict", func() bool { return ctxErr.Load() != nil })
	if err, _ := reportErr.Load().(error); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("report = %v, want a conflict", err)
	}
	if cause, _ := ctxErr.Load().(error); !errors.Is(cause, errLost) {
		t.Fatalf("handler context cause = %v, want the lost-job cause", cause)
	}
	time.Sleep(10 * time.Millisecond)
	if got := e.status(id); got != job.StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED left untouched by the runner", got)
	}
}

func TestReportAfterTheJobFinishedIsRefused(t *testing.T) {
	e := newEnv(t, nil, nil)
	late := make(chan func(context.Context, job.Progress) error, 1)
	e.register(typeStt, func(_ context.Context, _ job.Snapshot, report func(context.Context, job.Progress) error) error {
		late <- report
		return nil
	})
	id := e.enqueue(typeStt, 2)
	e.start()
	e.waitStatus(id, job.StatusCompleted)
	report := <-late
	p, _ := job.NewProgress(10, "")
	if err := report(context.Background(), p); !errors.Is(err, ErrJobFinished) || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("late report = %v, want ErrJobFinished (a conflict)", err)
	}
	if st := e.repo.snapshot(id); st.Percent != 100 {
		t.Fatalf("a late report changed the stored progress to %d", st.Percent)
	}
}

func TestOnlyRegisteredTypesAreClaimed(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		calls.Add(1)
		return nil
	})
	foreign := e.enqueue("other", 2)
	mine := e.enqueue(typeStt, 2)
	e.start()
	e.waitStatus(mine, job.StatusCompleted)
	e.idle(1)
	if got := e.status(foreign); got != job.StatusQueued {
		t.Fatalf("job of an unregistered type is %s, want QUEUED", got)
	}
}

func TestGracefulStopWaitsForRunningHandlers(t *testing.T) {
	e := newEnv(t, nil, nil)
	started, release := make(chan struct{}), make(chan struct{})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-release
		return nil
	})
	running := e.enqueue(typeStt, 2)
	e.start()
	<-started
	waiting := e.enqueue(typeStt, 2) // the only worker is busy; it must not be claimed after Stop

	stopped := make(chan error, 1)
	go func() { stopped <- e.svc.Stop(context.Background()) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned (%v) while a handler was running", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if got := e.status(running); got != job.StatusCompleted {
		t.Fatalf("running job is %s after a graceful stop, want COMPLETED", got)
	}
	if got := e.status(waiting); got != job.StatusQueued {
		t.Fatalf("second job is %s, want it untouched in QUEUED", got)
	}
}

func TestStopDeadlineInterruptsHandlersAndRequeuesTheirJobs(t *testing.T) {
	e := newEnv(t, nil, nil)
	started := make(chan struct{})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	id := e.enqueue(typeStt, 3)
	e.start()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the grace period is over at once
	if err := e.svc.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want the context error", err)
	}
	st := e.repo.snapshot(id)
	if st.Status != job.StatusQueued || st.Attempts != 1 {
		t.Fatalf("interrupted job: %s attempts=%d, want QUEUED with the attempt counted (like startup recovery)", st.Status, st.Attempts)
	}
}

func TestCancellingTheStartContextIsAForcedStop(t *testing.T) {
	e := newEnv(t, nil, nil)
	started := make(chan struct{})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	id := e.enqueue(typeStt, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	e.waitStatus(id, job.StatusQueued)
}

func TestWorkersNeverRunTheSameJobTwice(t *testing.T) {
	const jobs = 40
	e := newEnv(t, func(c *Config) { c.Workers = 6 }, nil)
	var mu sync.Mutex
	active := map[shared.JobID]int{}
	runs := map[shared.JobID]int{}
	var overlap atomic.Bool
	e.register(typeStt, func(_ context.Context, snap job.Snapshot, _ func(context.Context, job.Progress) error) error {
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
	})
	ids := make([]shared.JobID, jobs)
	for i := range ids {
		ids[i] = e.enqueue(typeStt, 2)
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
	defer mu.Unlock()
	for _, id := range ids {
		if runs[id] != 1 {
			t.Errorf("job %s ran %d times, want 1", id, runs[id])
		}
	}
	if overlap.Load() {
		t.Error("a job ran on two workers at once")
	}
}

func TestIdleWorkersPollOnTheInterval(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Workers = 3 }, nil)
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	e.start()
	e.idle(3)
	if got := e.clock.Sleepers(); got != 4 {
		t.Fatalf("sleepers = %d, want 3 workers and the promoter", got)
	}
}

func TestFinalSaveIsRetriedOnTransientErrors(t *testing.T) {
	errBusy := errors.New("database is locked")
	tests := []struct {
		name      string
		err       error
		failures  int // failing final saves in a row; negative means all of them
		wantSaves int32
		wantDone  bool
	}{
		{"fails once", errBusy, 1, 2, true},
		{"fails until the last attempt", errBusy, finalSaveAttempts - 1, finalSaveAttempts, true},
		{"fails every time", errBusy, -1, finalSaveAttempts, false},
		{"conflict is not retried", shared.ErrVersionConflict, -1, 1, false},
		{"missing job is not retried", shared.ErrNotFound, -1, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var finalSaves atomic.Int32
			e := newEnvOn(t, nil, nil, func(m *memRepo) JobRepository {
				return &hookRepo{memRepo: m, beforeSave: func(j *job.Job) error {
					if j.Status() == job.StatusRunning { // progress writes are not under test
						return nil
					}
					if n := int(finalSaves.Add(1)); tc.failures < 0 || n <= tc.failures {
						return tc.err
					}
					return nil
				}}
			})
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
			id := e.enqueue(typeStt, 2)
			e.start()

			e.drive("the final save attempts are made", func() bool { return finalSaves.Load() >= tc.wantSaves })
			if tc.wantDone {
				e.waitStatus(id, job.StatusCompleted)
			}
			for range 50 { // time goes on: nothing more may be tried
				e.clock.Advance(time.Minute)
			}
			time.Sleep(20 * time.Millisecond)
			if got := finalSaves.Load(); got != tc.wantSaves {
				t.Fatalf("final saves = %d, want %d", got, tc.wantSaves)
			}
			if !tc.wantDone && e.status(id) != job.StatusRunning {
				t.Fatalf("status = %s, want RUNNING left for startup recovery", e.status(id))
			}
		})
	}
}

func TestCancelReachesTheRunClaimedAfterARequeue(t *testing.T) {
	var (
		calls     atomic.Int32
		secondRun atomic.Bool
		cause     atomic.Value
		once      sync.Once
		saved     = make(chan struct{})
		release   = make(chan struct{})
	)
	e := newEnvOn(t, func(c *Config) { c.Workers = 2 }, nil, func(m *memRepo) JobRepository {
		return &hookRepo{memRepo: m, afterSave: func(j *job.Job) {
			if j.Status() != job.StatusRetryWait {
				return
			}
			// The first run has stored its retry but not yet left execute; hold it there.
			once.Do(func() {
				close(saved)
				<-release
			})
		}}
	})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		if calls.Add(1) == 1 {
			return errors.New("boom")
		}
		secondRun.Store(true)
		<-ctx.Done()
		cause.Store(context.Cause(ctx))
		return ctx.Err()
	})
	id := e.enqueue(typeStt, 3)
	e.start()

	<-saved
	e.drive("the requeued job is claimed by the other worker", secondRun.Load)
	close(release)

	// Three sleepers: the promoter, the second run's timer and the first worker, idle again once its execute returned.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.clock.AwaitSleepers(ctx, 3); err != nil {
		t.Fatalf("the first worker did not leave execute: %v", err)
	}

	if err := e.svc.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	e.waitStatus(id, job.StatusCancelled)
	if c, _ := cause.Load().(error); !errors.Is(c, errCancelled) {
		t.Fatalf("handler of the second run saw cause %v, want the user cancellation", c)
	}
}

func TestStoredErrorTextHasNoPanicValue(t *testing.T) {
	const marker = "SECRET-TRANSCRIPT-TEXT"
	tests := []struct {
		name     string
		handle   func() error
		want     job.Status
		contains []string
		absent   string
	}{
		{"panic with a string", func() error { panic(marker) }, job.StatusFailed, []string{"handler panic", "string"}, marker},
		{"panic with an error", func() error { panic(errors.New(marker)) }, job.StatusFailed, []string{"handler panic", "*errors.errorString"}, marker},
		{"ordinary error keeps its message", func() error { return errors.New("upstream said no: " + marker) }, job.StatusRetryWait, []string{"upstream said no", marker}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				return tc.handle()
			})
			id := e.enqueue(typeStt, 3)
			e.start()
			e.waitStatus(id, tc.want)

			got := e.repo.snapshot(id).LastError
			for _, s := range tc.contains {
				if !strings.Contains(got, s) {
					t.Errorf("lastError = %q, want it to contain %q", got, s)
				}
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("lastError = %q leaks %q", got, tc.absent)
			}
		})
	}
}

func TestInterruptedJobWithoutAttemptsFailsAndIsLoggedAsFailed(t *testing.T) {
	e := newEnv(t, nil, nil)
	var logs bytes.Buffer // read only after Stop has waited for the workers
	e.svc.log = slog.New(slog.NewTextHandler(&logs, nil))
	started := make(chan struct{})
	e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	id := e.enqueue(typeStt, 1)
	e.start()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.svc.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v", err)
	}
	st := e.repo.snapshot(id)
	if st.Status != job.StatusFailed || !strings.HasPrefix(st.LastError, "attempts exhausted") {
		t.Fatalf("job: %s %q, want FAILED with the attempts exhausted text", st.Status, st.LastError)
	}
	if out := logs.String(); !strings.Contains(out, "outcome=failed") || strings.Contains(out, "outcome=interrupted") {
		t.Fatalf("log does not show the real outcome:\n%s", out)
	}
}
