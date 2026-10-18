package jobrunner

import (
	"context"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/domain/jobs/job"
)

// Activate registers a handler for a type that has none, also after Start: a capability that appears at run time,
// such as the speech component installed from the app (TASK-072), starts its jobs without a restart. Register, Recover
// and Start keep their contract for everything else.
//
// Before the type becomes claimable, the jobs of that type a previous process left RUNNING are recovered with the
// Recovery policy of r, exactly like Recover does. That is safe while workers run: they claim and run only registered
// types, so no live worker of this process owns such a job. Then the claim loop and the power policy are rebuilt with
// the new type and the idle workers are woken, so its QUEUED jobs start at once.
//
// It waits for a running Start or Recover. Errors: ErrInvalidConfig for an unusable registration, ErrAlreadyRegistered
// (with ErrInvalidConfig) when the type has a handler, ErrStopped after Stop, and the listing or ctx error; in all those
// cases the type stays unregistered. Before Start it registers the type like Register after recovering its jobs.
func (s *Service) Activate(ctx context.Context, r Registration) (RecoveryReport, error) {
	var rep RecoveryReport
	if err := r.validate(); err != nil {
		return rep, err
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	if err := s.checkActivatable(r.Type); err != nil {
		return rep, err
	}

	stale, _, err := s.jobs.ListByStatusTolerant(ctx, job.StatusRunning)
	if err != nil {
		return rep, fmt.Errorf("listing stale jobs: %w", err)
	}
	regs := map[job.Type]Registration{r.Type: r}
	for _, j := range stale {
		if j.Type() != r.Type {
			continue
		}
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		s.recoverJob(ctx, j, regs, &rep)
	}

	if err := s.add(r); err != nil {
		return rep, err
	}
	s.log.Info("job type activated", slog.String("type", string(r.Type)), slog.Int("requeued", len(rep.Requeued)),
		slog.Int("failed", len(rep.Failed)), slog.Int("errors", len(rep.Errors)))
	return rep, nil
}

// checkActivatable refuses a type with a handler and a stopped runner.
func (s *Service) checkActivatable(t job.Type) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.handlers[t]; dup {
		return errDuplicate(t)
	}
	if s.loopCtx.Err() != nil {
		return ErrStopped
	}
	return nil
}

// add registers r and, once started, publishes the new classes and wakes the loops. The checks are repeated under mu:
// Register before Start does not take the lifecycle lock.
func (s *Service) add(r Registration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.handlers[r.Type]; dup {
		return errDuplicate(r.Type)
	}
	if s.loopCtx.Err() != nil {
		return ErrStopped
	}
	if !s.started {
		s.handlers[r.Type] = r
		return nil
	}

	s.handlers[r.Type] = r
	c, err := buildClasses(s.handlers)
	if err != nil {
		delete(s.handlers, r.Type)
		return err
	}
	s.classes.Store(c)
	s.wakeLocked()
	return nil
}
