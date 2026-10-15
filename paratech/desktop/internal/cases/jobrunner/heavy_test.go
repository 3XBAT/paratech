package jobrunner

import (
	"context"
	"sync/atomic"
	"testing"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/testenv"
)

// Two workers, two heavy jobs and a light one: never two heavy jobs at once, and the light one still runs while a
// heavy job holds the only slot.
func TestHeavyJobsRunOneAtATime(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Workers = 2 }, nil)

	var running, peak atomic.Int32
	entered := make(chan job.Snapshot, 2)
	release := make(chan struct{})
	heavy := handlerFunc(func(ctx context.Context, snap job.Snapshot, _ func(context.Context, job.Progress) error) error {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		entered <- snap
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	if err := e.svc.Register(Registration{Type: typeStt, Handler: heavy, Heavy: true, Recovery: RecoveryRequeue}); err != nil {
		t.Fatal(err)
	}
	e.register(typeMail, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })

	first, second := e.enqueue(typeStt, 3), e.enqueue(typeStt, 3)
	light := e.enqueue(typeMail, 3)
	e.start()

	<-entered
	e.waitStatus(light, job.StatusCompleted)
	// The free worker found nothing it may claim and sleeps (with the keeper and the timer of the heavy attempt); the
	// clock does not move.
	e.idle(2)
	if got := running.Load(); got != 1 {
		t.Fatalf("%d heavy jobs run, want 1", got)
	}
	queued := 0
	for _, s := range []job.Status{e.status(first), e.status(second)} {
		if s == job.StatusQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("statuses %s and %s: want one heavy job left QUEUED", e.status(first), e.status(second))
	}

	release <- struct{}{} // the first heavy job ends; its worker takes the second at once
	<-entered
	release <- struct{}{}
	e.waitStatus(first, job.StatusCompleted)
	e.waitStatus(second, job.StatusCompleted)
	if p := peak.Load(); p != 1 {
		t.Fatalf("peak %d heavy jobs at once, want 1", p)
	}
}

// MaxHeavy defaults to one and must fit the pool.
func TestMaxHeavyValidation(t *testing.T) {
	repo, clock := newMemRepo(), testenv.NewFakeClock(t0)
	deps := Deps{Jobs: repo, Clock: clock, Sleeper: clock}
	tests := []struct {
		name     string
		workers  int
		maxHeavy int
		ok       bool
	}{
		{"zero means the default", 2, 0, true},
		{"one per pool", 1, 1, true},
		{"as many as workers", 3, 3, true},
		{"negative", 2, -1, false},
		{"more than workers", 2, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Workers, cfg.MaxHeavy = tt.workers, tt.maxHeavy
			svc, err := New(deps, cfg)
			if (err == nil) != tt.ok {
				t.Fatalf("New = %v, want ok=%v", err, tt.ok)
			}
			if err == nil && tt.maxHeavy == 0 && svc.cfg.MaxHeavy != DefaultMaxHeavy {
				t.Fatalf("MaxHeavy = %d, want the default %d", svc.cfg.MaxHeavy, DefaultMaxHeavy)
			}
		})
	}
	if DefaultConfig().MaxHeavy != 1 {
		t.Fatalf("DefaultConfig().MaxHeavy = %d, want 1", DefaultConfig().MaxHeavy)
	}
}
