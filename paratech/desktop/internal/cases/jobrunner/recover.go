package jobrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Last errors of jobs that recovery fails without asking a handler.
const (
	msgRecoveryFail      = "interrupted; this job type is not run again after an interruption"
	msgNoReconciler      = "interrupted with an unknown outcome; not run again without reconciliation"
	msgReconciledAsFails = "interrupted; reconciliation decided not to run it again"
)

// reconcileTimeout bounds one Reconcile call: Recover holds up Start, so a hung lookup must not block the app start.
const reconcileTimeout = 30 * time.Second

// Outcomes of recovering one stale job, for the log.
const (
	outcomeRequeued     = "requeued"
	outcomeUnregistered = "unregistered"
	outcomeCorrupt      = "corrupt"
	outcomeConflict     = "conflict"
	outcomeError        = "error"
)

// Recover handles the jobs a previous process left RUNNING (PRD section 7.2: startup recovery, AC-09). Call it after
// the handlers are registered and before Start; it returns ErrAlreadyStarted once workers run, because then a RUNNING
// job may belong to a live worker. Each job is handled by the Recovery policy of its type:
//
//   - RecoveryRequeue: back to QUEUED through the aggregate (the crashed attempt counts; no attempt left: FAILED);
//   - RecoveryReconcile: the Reconciler of the type decides COMPLETED, QUEUED or FAILED; without one the job fails,
//     it is never resent blindly (hard rule 7, AC-15). Each call is bounded by reconcileTimeout; a timeout leaves the
//     job RUNNING with ErrReconcileTimeout in the report;
//   - RecoveryFail: FAILED.
//
// A job of an unregistered type is left untouched, so a type registered by a later build still recovers it. Every job
// is independent: a corrupt row, a refused reconciliation, a storage error or a version conflict skips that job and
// is recorded in the report. Recover is idempotent: a second call finds nothing left to do except the jobs it skipped.
// The returned error is for the whole call only (the listing failed, ctx ended, wrong lifecycle state); the report
// then holds what was done before it.
func (s *Service) Recover(ctx context.Context) (RecoveryReport, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	var rep RecoveryReport
	s.mu.Lock()
	started, regs := s.started, maps.Clone(s.handlers)
	s.mu.Unlock()
	switch {
	case started:
		return rep, ErrAlreadyStarted
	case s.loopCtx.Err() != nil:
		return rep, ErrStopped
	}

	stale, corrupt, err := s.jobs.ListByStatusTolerant(ctx, job.StatusRunning)
	if err != nil {
		return rep, fmt.Errorf("listing stale jobs: %w", err)
	}
	for _, id := range corrupt {
		s.log.Warn("stale job row is corrupt, left as it is", slog.String("job", id.String()), slog.String("outcome", outcomeCorrupt))
	}
	rep.Corrupt = corrupt

	for _, j := range stale {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		s.recoverJob(ctx, j, regs, &rep)
	}

	s.log.Info("startup recovery finished",
		slog.Int("requeued", len(rep.Requeued)), slog.Int("completed", len(rep.Completed)), slog.Int("failed", len(rep.Failed)),
		slog.Int("unregistered", len(rep.Unregistered)), slog.Int("corrupt", len(rep.Corrupt)),
		slog.Int("conflicts", len(rep.Conflicts)), slog.Int("errors", len(rep.Errors)))
	return rep, nil
}

// recoverJob applies the policy of the job's type, saves the job and records the outcome in rep.
func (s *Service) recoverJob(ctx context.Context, j *job.Job, regs map[job.Type]Registration, rep *RecoveryReport) {
	id, typ := j.ID(), j.Type()
	reg, ok := regs[typ]
	if !ok {
		s.log.Warn("stale job of an unregistered type, left RUNNING", slog.String("job", id.String()),
			slog.String("type", string(typ)), slog.String("outcome", outcomeUnregistered))
		rep.Unregistered = append(rep.Unregistered, id)
		return
	}

	fail := func(err error) {
		s.log.Error("recovery of a stale job failed, left RUNNING", slog.String("job", id.String()),
			slog.String("type", string(typ)), slog.String("outcome", outcomeError), slog.Any("error", err))
		rep.Errors = append(rep.Errors, RecoveryError{Job: id, Type: typ, Err: err})
	}

	if err := s.applyRecovery(ctx, j, reg); err != nil {
		fail(err)
		return
	}

	// One save, no retry loop: a job left RUNNING is safe and the next Recover tries again.
	switch err := s.jobs.Save(ctx, j); {
	case errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound):
		s.log.Info("stale job was changed elsewhere, skipped", slog.String("job", id.String()),
			slog.String("type", string(typ)), slog.String("outcome", outcomeConflict))
		rep.Conflicts = append(rep.Conflicts, id)
		return
	case err != nil:
		fail(err)
		return
	}

	outcome := outcomeFailed
	switch j.Status() {
	case job.StatusQueued:
		outcome, rep.Requeued = outcomeRequeued, append(rep.Requeued, id)
	case job.StatusCompleted:
		outcome, rep.Completed = outcomeCompleted, append(rep.Completed, id)
	default:
		rep.Failed = append(rep.Failed, id)
	}
	s.log.Info("stale job recovered", slog.String("job", id.String()), slog.String("type", string(typ)),
		slog.String("outcome", outcome), slog.String("status", j.Status().String()))
	s.jobFinished(ctx, j.Snapshot())
}

// applyRecovery moves the stale job through the aggregate according to the policy of its type.
func (s *Service) applyRecovery(ctx context.Context, j *job.Job, reg Registration) error {
	switch reg.Recovery {
	case RecoveryRequeue:
		return j.Requeue(s.clock.Now())
	case RecoveryFail:
		return j.Fail(s.clock.Now(), msgRecoveryFail)
	case RecoveryReconcile:
		if reg.Reconciler == nil {
			return j.Fail(s.clock.Now(), msgNoReconciler)
		}
		next, reason, err := s.reconcile(ctx, reg.Reconciler, j.Snapshot())
		if err != nil {
			return err
		}
		return applyReconciled(j, next, reason, s.clock.Now())
	}
	return fmt.Errorf("%w: no recovery policy for %q", ErrInvalidConfig, j.Type())
}

// applyReconciled turns the reconciler's answer into the aggregate transition.
func applyReconciled(j *job.Job, next job.Status, reason string, now time.Time) error {
	switch next {
	case job.StatusCompleted:
		return j.Complete(now)
	case job.StatusQueued:
		return j.Requeue(now)
	case job.StatusFailed:
		if reason == "" {
			reason = msgReconciledAsFails
		}
		return j.Fail(now, reason)
	}
	return fmt.Errorf("%w: %q", ErrInvalidReconciliation, next)
}

// reconcile calls the hook under reconcileTimeout and turns a panic into an error, so one broken or hung reconciler
// does not stop the recovery. A timeout is an error like any other: the job stays RUNNING.
func (s *Service) reconcile(ctx context.Context, rc Reconciler, snap job.Snapshot) (next job.Status, reason string, err error) {
	rctx, cancel := context.WithCancelCause(ctx)
	timerDone := make(chan struct{})
	go func() {
		defer close(timerDone)
		if s.sleeper.Sleep(rctx, reconcileTimeout) == nil {
			cancel(errReconcileTimeout)
		}
	}()
	defer func() {
		cancel(errFinished)
		<-timerDone
	}()

	defer func() {
		if p := recover(); p != nil {
			s.log.Error("job reconciler panicked", slog.String("job", snap.ID().String()), slog.String("type", fmt.Sprintf("%T", p)),
				slog.Any("panic", p))
			next, reason, err = "", "", fmt.Errorf("reconciler panic: %T", p)
		}
	}()

	next, reason, err = rc.Reconcile(rctx, snap)
	if err != nil && errors.Is(context.Cause(rctx), errReconcileTimeout) {
		err = fmt.Errorf("%w after %v: %w", ErrReconcileTimeout, reconcileTimeout, err)
	}
	return next, reason, err
}
