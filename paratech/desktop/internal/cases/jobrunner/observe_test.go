package jobrunner

import (
	"bytes"
	"context"
	"errors"
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

// recObserver records every JobFinished call; fn, if set, decides what the call does.
type recObserver struct {
	mu    sync.Mutex
	snaps []job.Snapshot
	fn    func(ctx context.Context, snap job.Snapshot) error
}

func (o *recObserver) JobFinished(ctx context.Context, snap job.Snapshot) error {
	o.mu.Lock()
	o.snaps = append(o.snaps, snap)
	fn := o.fn
	o.mu.Unlock()
	if fn != nil {
		return fn(ctx, snap)
	}
	return nil
}

func (o *recObserver) calls() []job.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]job.Snapshot(nil), o.snaps...)
}

// newObservedEnv is newEnv with an observer in Deps and the log in w (nil discards it).
func newObservedEnv(t *testing.T, obs Observer, w *syncWriter) *env {
	t.Helper()
	testenv.NoGoroutineLeaks(t)
	e := &env{t: t, repo: newMemRepo(), clock: testenv.NewFakeClock(t0)}
	log := slog.New(slog.DiscardHandler)
	if w != nil {
		log = slog.New(slog.NewTextHandler(w, nil))
	}
	svc, err := New(Deps{Jobs: e.repo, Clock: e.clock, Sleeper: e.clock, Observer: obs, Log: log}, testConfig())
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

// syncWriter serialises writes of the logger, which runs on several goroutines.
type syncWriter struct {
	mu sync.Mutex
	w  bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.String()
}

func requireCalls(t *testing.T, obs *recObserver, want ...job.Status) []job.Snapshot {
	t.Helper()
	eventually(t, "the observer calls", func() bool { return len(obs.calls()) >= len(want) })
	got := obs.calls()
	if len(got) != len(want) {
		t.Fatalf("%d observer calls, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Status() != want[i] {
			t.Fatalf("call %d with status %s, want %s", i, s.Status(), want[i])
		}
	}
	return got
}

func TestObserverHearsCompletionOnce(t *testing.T) {
	obs := &recObserver{}
	e := newObservedEnv(t, obs, nil)
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	id := e.enqueue(typeStt, 3)
	e.start()

	e.waitStatus(id, job.StatusCompleted)
	got := requireCalls(t, obs, job.StatusCompleted)
	if got[0].ID() != id || got[0].Type() != typeStt || got[0].Attempt() != 1 {
		t.Fatalf("snapshot = %s %s attempt %d, want %s %s attempt 1", got[0].ID(), got[0].Type(), got[0].Attempt(), id, typeStt)
	}

	e.idle(1)
	for range 5 {
		e.clock.Advance(time.Second)
	}
	e.idle(1)
	if n := len(obs.calls()); n != 1 {
		t.Fatalf("%d observer calls after more polls, want 1", n)
	}
}

func TestObserverHearsFailureButNotRetries(t *testing.T) {
	obs := &recObserver{}
	e := newObservedEnv(t, obs, nil)
	var runs atomic.Int32
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		if runs.Add(1) < 3 {
			return errors.New("transient")
		}
		return backoff.Permanent(errors.New("broken input"))
	})
	id := e.enqueue(typeStt, 5)
	e.start()

	e.drive("the job to fail", func() bool { return e.status(id) == job.StatusFailed })
	if got := statuses(e.repo.path(id)); !equalStatuses(got[len(got)-1:], []job.Status{job.StatusFailed}) || runs.Load() != 3 {
		t.Fatalf("path = %v after %d runs", got, runs.Load())
	}
	requireCalls(t, obs, job.StatusFailed) // the two RETRY_WAIT states were not reported
}

func TestObserverIsNotToldAboutWaits(t *testing.T) {
	obs := &recObserver{}
	e := newObservedEnv(t, obs, nil)
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error {
		return shared.ErrNetworkUnavailable
	})
	id := e.enqueue(typeStt, 3)
	e.start()

	e.waitStatus(id, job.StatusWaitingForNetwork)
	e.idle(1)
	if n := len(obs.calls()); n != 0 {
		t.Fatalf("%d observer calls for a waiting job, want 0", n)
	}
}

func TestObserverHearsCancellations(t *testing.T) {
	t.Run("queued job", func(t *testing.T) {
		obs := &recObserver{}
		e := newObservedEnv(t, obs, nil)
		id := e.enqueue(typeStt, 3)
		if err := e.svc.Cancel(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if got := requireCalls(t, obs, job.StatusCancelled); got[0].ID() != id {
			t.Fatalf("snapshot of %s, want %s", got[0].ID(), id)
		}
	})

	t.Run("running job", func(t *testing.T) {
		obs := &recObserver{}
		e := newObservedEnv(t, obs, nil)
		started := make(chan struct{})
		e.register(typeStt, func(ctx context.Context, _ job.Snapshot, _ func(context.Context, job.Progress) error) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		id := e.enqueue(typeStt, 3)
		e.start()
		<-started
		if err := e.svc.Cancel(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		e.waitStatus(id, job.StatusCancelled)
		requireCalls(t, obs, job.StatusCancelled)
	})
}

func TestObserverHearsRecoveredJobsThatFinished(t *testing.T) {
	obs := &recObserver{}
	e := newObservedEnv(t, obs, nil)
	e.registerRecovery(typeStt, RecoveryRequeue, nil)
	e.registerRecovery(typeMail, RecoveryFail, nil)
	requeued := e.seedRunning(typeStt, 1, 3)
	failed := e.seedRunning(typeMail, 1, 3)

	rep := e.recover()
	if len(rep.Requeued) != 1 || rep.Requeued[0] != requeued || len(rep.Failed) != 1 || rep.Failed[0] != failed {
		t.Fatalf("report = %+v", rep)
	}
	if got := requireCalls(t, obs, job.StatusFailed); got[0].ID() != failed {
		t.Fatalf("observer heard %s, want only the failed %s", got[0].ID(), failed)
	}
}

func TestObserverPanicOrErrorDoesNotBreakTheRunner(t *testing.T) {
	var logs syncWriter
	var calls atomic.Int32
	obs := &recObserver{fn: func(context.Context, job.Snapshot) error {
		if calls.Add(1) == 1 {
			panic("observer bug")
		}
		return errors.New("observer down")
	}}
	e := newObservedEnv(t, obs, &logs)
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	first := e.enqueue(typeStt, 3)
	second := e.enqueue(typeStt, 3)
	e.start()

	// The worker survives the panic and runs the next job.
	e.drive("both jobs to complete", func() bool {
		return e.status(first) == job.StatusCompleted && e.status(second) == job.StatusCompleted
	})
	requireCalls(t, obs, job.StatusCompleted, job.StatusCompleted)
	eventually(t, "both log lines", func() bool {
		s := logs.String()
		return strings.Contains(s, "job observer panicked") && strings.Contains(s, "observer down")
	})
}

func TestObserverCallIsBounded(t *testing.T) {
	causes := make(chan error, 1)
	obs := &recObserver{fn: func(ctx context.Context, _ job.Snapshot) error {
		<-ctx.Done()
		causes <- context.Cause(ctx)
		return ctx.Err()
	}}
	e := newObservedEnv(t, obs, nil)
	e.register(typeStt, func(context.Context, job.Snapshot, func(context.Context, job.Progress) error) error { return nil })
	first := e.enqueue(typeStt, 3)
	second := e.enqueue(typeStt, 3)
	e.start()

	e.waitStatus(first, job.StatusCompleted)
	e.drive("the observer bound", func() bool { return len(causes) == 1 })
	if cause := <-causes; !errors.Is(cause, errObserverTimeout) {
		t.Fatalf("observer context ended with %v, want the observer bound", cause)
	}
	// The worker is free again: the second job runs (its observer call is bounded the same way).
	e.drive("the second job", func() bool { return e.status(second) == job.StatusCompleted })
	e.drive("the second bound", func() bool { return len(causes) == 1 })
	<-causes
}
