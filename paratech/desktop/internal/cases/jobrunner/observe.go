package jobrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
)

// observerTimeout bounds one Observer call: it runs on a worker, in Cancel or in Recover and must not hold them up.
const observerTimeout = 5 * time.Second

// jobFinished tells the observer that a job reached a terminal status. The job is already saved, so whatever the
// observer does, an error, a panic or running into the bound, is only logged.
func (s *Service) jobFinished(parent context.Context, snap job.Snapshot) {
	if s.observer == nil || !snap.Status().IsTerminal() {
		return
	}

	// A shutdown or a caller that gives up must not cut the call short; only the bound ends it, so a forced Stop
	// waits up to observerTimeout for a call in flight.
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	timerDone := make(chan struct{})
	go func() {
		defer close(timerDone)
		if s.sleeper.Sleep(ctx, observerTimeout) == nil {
			cancel(errObserverTimeout)
		}
	}()
	defer func() {
		cancel(errFinished)
		<-timerDone
	}()

	defer func() {
		if p := recover(); p != nil {
			s.log.Error("job observer panicked", slog.String("job", snap.ID().String()), slog.String("type", fmt.Sprintf("%T", p)),
				slog.Any("panic", p))
		}
	}()
	if err := s.observer.JobFinished(ctx, snap); err != nil {
		s.log.Warn("job observer failed", slog.String("job", snap.ID().String()), slog.String("status", snap.Status().String()),
			slog.Any("error", err))
	}
}
