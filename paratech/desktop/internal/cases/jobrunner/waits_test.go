package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// Network and quota waits of LLM jobs (PRD sections 7.2 and 9.2, TASK-042).

func TestNetworkAndQuotaErrorsWait(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		want     job.Status
		attempts int
	}{
		{"network", fmt.Errorf("hub: %w", shared.ErrNetworkUnavailable), job.StatusWaitingForNetwork, 0},
		{"network marked transient", backoff.Transient(shared.ErrNetworkUnavailable), job.StatusWaitingForNetwork, 0},
		{"quota", fmt.Errorf("hub: %w", shared.ErrQuotaExceeded), job.StatusWaitingForQuota, 0},
		{"quota marked permanent fails", backoff.Permanent(shared.ErrQuotaExceeded), job.StatusFailed, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.register(typeMail, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				return tc.err
			})
			id := e.enqueue(typeMail, 3)
			e.start()
			e.waitStatus(id, tc.want)
			if st := e.repo.snapshot(id); st.Attempts != tc.attempts {
				t.Fatalf("attempts = %d, want %d (waiting gives the attempt back)", st.Attempts, tc.attempts)
			}
		})
	}
}

func TestWaitingJobIsRetriedAfterTheRecheck(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wait    job.Status
		recheck time.Duration
	}{
		{"network", shared.ErrNetworkUnavailable, job.StatusWaitingForNetwork, 10 * time.Second},
		{"quota", shared.ErrQuotaExceeded, job.StatusWaitingForQuota, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, func(c *Config) {
				c.Rechecks = backoff.Rechecks{Network: 10 * time.Second, Quota: 30 * time.Second}
			}, nil)
			var calls atomic.Int32
			e.register(typeMail, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
				if calls.Add(1) == 1 {
					return tc.err
				}
				return nil
			})
			id := e.enqueue(typeMail, 1)
			e.start()
			e.waitStatus(id, tc.wait)
			e.idle(1)

			e.clock.Advance(tc.recheck - time.Second) // before the recheck: polls pass, nothing runs
			e.idle(1)
			if got, status := calls.Load(), e.status(id); got != 1 || status != tc.wait {
				t.Fatalf("calls=%d status=%s before the recheck, want 1 and %s", got, status, tc.wait)
			}

			// One attempt was allowed: the wait gave it back, so the job can still complete.
			e.drive("the job completes", func() bool { return e.status(id) == job.StatusCompleted })
			want := []job.Status{job.StatusQueued, job.StatusRunning, tc.wait, job.StatusQueued, job.StatusRunning, job.StatusCompleted}
			if got := statuses(e.repo.path(id)); !equalStatuses(got, want) {
				t.Fatalf("path = %v, want %v", got, want)
			}
		})
	}
}

func TestRepeatedWaitsDoNotExhaustAttempts(t *testing.T) {
	e := newEnv(t, nil, nil)
	var calls atomic.Int32
	e.register(typeMail, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		if calls.Add(1) <= 3 {
			return shared.ErrNetworkUnavailable
		}
		return nil
	})
	id := e.enqueue(typeMail, 1)
	e.start()
	testenv.Eventually(t, "the job completes", func() bool { return e.status(id) == job.StatusCompleted },
		func() { e.clock.Advance(30 * time.Second) })
	if got := calls.Load(); got != 4 {
		t.Fatalf("calls = %d, want 4", got)
	}
}

// A shutdown or a timeout is an interruption, not a wait, even when the handler reports no network.
func TestTimeoutWithNetworkErrorRetries(t *testing.T) {
	e := newEnv(t, nil, nil)
	if err := e.svc.Register(Registration{
		Type: typeMail, Recovery: RecoveryRequeue, Timeout: 5 * time.Second,
		Handler: handlerFunc(func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
			<-ctx.Done()
			return fmt.Errorf("%w: %w", shared.ErrNetworkUnavailable, ctx.Err())
		}),
	}); err != nil {
		t.Fatal(err)
	}
	id := e.enqueue(typeMail, 3)
	e.start()
	e.drive("the attempt times out", func() bool { return e.status(id) == job.StatusRetryWait })
}

func TestNewValidatesRechecks(t *testing.T) {
	repo, clock := newMemRepo(), testenv.NewFakeClock(t0)
	deps := Deps{Jobs: repo, Clock: clock, Sleeper: clock}
	for name, r := range map[string]backoff.Rechecks{
		"no network wait": {Quota: time.Minute},
		"negative quota":  {Network: time.Minute, Quota: -time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Rechecks = r
			if _, err := New(deps, cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New = %v, want ErrInvalidConfig", err)
			}
		})
	}
	cfg := testConfig()
	cfg.Rechecks = backoff.Rechecks{}
	if _, err := New(deps, cfg); err != nil {
		t.Fatalf("zero rechecks must take the defaults: %v", err)
	}
}
