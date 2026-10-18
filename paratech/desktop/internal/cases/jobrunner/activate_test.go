package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/powerpolicy"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

func okHandler() handlerFunc {
	return func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil }
}

// A type activated after Start runs its waiting jobs at once (the workers are woken; the clock does not move) and a
// job of that type left RUNNING by a previous process is recovered first, by the type's policy.
func TestActivateAfterStartRunsQueuedAndRecoversStaleJobs(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Workers = 2 }, nil)
	e.register(typeMail, okHandler())
	queued := e.enqueue(typeStt, 3)
	stale := e.seedRunning(typeStt, 1, 3)
	other := e.seedRunning(typeLegacy, 1, 3)
	if rep := e.recover(); len(rep.Unregistered) != 2 {
		t.Fatalf("startup recovery: %+v, want both stale jobs unregistered", rep)
	}
	e.start()
	e.idle(2)
	if e.status(queued) != job.StatusQueued {
		t.Fatalf("queued job = %s before Activate, want QUEUED", e.status(queued))
	}

	var calls atomic.Int32
	rep, err := e.svc.Activate(context.Background(), Registration{
		Type: typeStt, Handler: countingHandler(&calls), Heavy: true, Recovery: RecoveryRequeue,
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := bucketOf(t, rep, stale); got != "requeued" {
		t.Fatalf("stale stt job in %q, want requeued", got)
	}
	e.waitStatus(queued, job.StatusCompleted)
	e.waitStatus(stale, job.StatusCompleted)
	if calls.Load() != 2 {
		t.Fatalf("stt handler ran %d times, want 2", calls.Load())
	}
	if e.status(other) != job.StatusRunning {
		t.Fatalf("job of another unregistered type = %s, want it left RUNNING", e.status(other))
	}
}

// RecoveryFail of the activated type fails its stale job instead of running it.
func TestActivateUsesTheRecoveryPolicyOfTheType(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.register(typeMail, okHandler())
	stale := e.seedRunning(typeStt, 1, 3)
	e.start()

	var calls atomic.Int32
	rep, err := e.svc.Activate(context.Background(), Registration{Type: typeStt, Handler: countingHandler(&calls), Recovery: RecoveryFail})
	if err != nil {
		t.Fatal(err)
	}
	if got := bucketOf(t, rep, stale); got != "failed" {
		t.Fatalf("stale job in %q, want failed", got)
	}
	if e.status(stale) != job.StatusFailed || calls.Load() != 0 {
		t.Fatalf("status %s, %d calls; want FAILED and no call", e.status(stale), calls.Load())
	}
}

func TestActivateRefusals(t *testing.T) {
	valid := Registration{Type: typeStt, Handler: okHandler(), Recovery: RecoveryRequeue}
	tests := []struct {
		name    string
		prepare func(e *env)
		reg     Registration
		want    []error
	}{
		{"registered before start", func(e *env) { e.register(typeStt, okHandler()); e.start() }, valid,
			[]error{ErrInvalidConfig, ErrAlreadyRegistered}},
		{"activated twice", func(e *env) {
			e.register(typeMail, okHandler())
			e.start()
			if _, err := e.svc.Activate(context.Background(), valid); err != nil {
				e.t.Fatal(err)
			}
		}, valid, []error{ErrInvalidConfig, ErrAlreadyRegistered}},
		{"stopped", func(e *env) {
			e.register(typeMail, okHandler())
			e.start()
			if err := e.svc.Stop(context.Background()); err != nil {
				e.t.Fatal(err)
			}
		}, valid, []error{ErrStopped}},
		{"no handler", func(e *env) { e.register(typeMail, okHandler()); e.start() },
			Registration{Type: typeStt, Recovery: RecoveryRequeue}, []error{ErrInvalidConfig}},
		{"no recovery policy", func(e *env) { e.register(typeMail, okHandler()); e.start() },
			Registration{Type: typeStt, Handler: okHandler()}, []error{ErrInvalidConfig}},
		{"no type", func(e *env) { e.register(typeMail, okHandler()); e.start() },
			Registration{Handler: okHandler(), Recovery: RecoveryRequeue}, []error{ErrInvalidConfig}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			stale := e.seedRunning(typeStt, 1, 3)
			tt.prepare(e)
			_, err := e.svc.Activate(context.Background(), tt.reg)
			for _, want := range tt.want {
				if !errors.Is(err, want) {
					t.Fatalf("Activate = %v, want %v", err, want)
				}
			}
			if tt.name != "activated twice" && e.status(stale) != job.StatusRunning {
				t.Fatalf("refused Activate changed the stale job to %s", e.status(stale))
			}
		})
	}
}

// A failed listing leaves the type unregistered, so a later Activate can try again.
func TestActivateListingFailureRegistersNothing(t *testing.T) {
	var hook *hookRepo
	e := newEnvOn(t, nil, nil, func(r *memRepo) JobRepository { hook = &hookRepo{memRepo: r}; return hook })
	e.register(typeMail, okHandler())
	queued := e.enqueue(typeStt, 3)
	e.start()

	hook.listErr = errors.New("disk busy")
	reg := Registration{Type: typeStt, Handler: okHandler(), Recovery: RecoveryRequeue}
	if _, err := e.svc.Activate(context.Background(), reg); err == nil {
		t.Fatal("Activate with a failing listing succeeded")
	}
	hook.listErr = nil
	if _, err := e.svc.Activate(context.Background(), reg); err != nil {
		t.Fatalf("second Activate: %v", err)
	}
	e.waitStatus(queued, job.StatusCompleted)
}

// Before Start, Activate registers like Register; Start then runs the type.
func TestActivateBeforeStart(t *testing.T) {
	e := newEnv(t, nil, nil)
	stale := e.seedRunning(typeStt, 1, 3)
	queued := e.enqueue(typeStt, 3)
	if _, err := e.svc.Activate(context.Background(), Registration{Type: typeStt, Handler: okHandler(), Recovery: RecoveryRequeue}); err != nil {
		t.Fatal(err)
	}
	if e.status(stale) != job.StatusQueued {
		t.Fatalf("stale job = %s, want QUEUED", e.status(stale))
	}
	e.start()
	e.waitStatus(queued, job.StatusCompleted)
	e.waitStatus(stale, job.StatusCompleted)
}

// An activated Heavy type follows the power policy: on battery with the setting on its job waits for power.
func TestActivatedHeavyTypeFollowsThePowerPolicy(t *testing.T) {
	power := testenv.NewFakePower(false)
	e := newPowerEnv(t, power, newFakeSettings(true))
	e.register(typeMail, okHandler())
	stt := e.enqueue(typeStt, 3)
	e.start()
	e.awaitSource(powerpolicy.SourceBattery)

	var calls atomic.Int32
	if _, err := e.svc.Activate(context.Background(), Registration{
		Type: typeStt, Handler: countingHandler(&calls), Heavy: true, Recovery: RecoveryRequeue,
	}); err != nil {
		t.Fatal(err)
	}
	e.waitStatus(stt, job.StatusWaitingForPower)
	if calls.Load() != 0 {
		t.Fatalf("heavy job ran %d times on battery", calls.Load())
	}

	power.Set(true)
	e.drive("the heavy job completes on AC", func() bool { return e.status(stt) == job.StatusCompleted })
}

// Activations race with running workers, power events and each other; run with -race.
func TestConcurrentActivations(t *testing.T) {
	power := testenv.NewFakePower(true)
	e := newPowerEnv(t, power, newFakeSettings(true))
	e.register(typeMail, okHandler())
	const n = 6
	var ids []shared.JobID
	for i := range n {
		ids = append(ids, e.enqueue(job.Type(fmt.Sprintf("late-%d", i)), 3))
		e.enqueue(typeMail, 3)
	}
	e.start()

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Activate(context.Background(), Registration{
				Type: job.Type(fmt.Sprintf("late-%d", i)), Handler: okHandler(), Heavy: i%2 == 0, Recovery: RecoveryRequeue,
			}); err != nil {
				t.Errorf("Activate %d: %v", i, err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 10 {
			power.Set(i%2 == 0)
		}
	}()
	wg.Wait()
	power.Set(true)

	for _, id := range ids {
		e.drive("activated job completes", func() bool { return e.status(id) == job.StatusCompleted })
	}
}
