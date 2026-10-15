package jobrunner

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports the Service needs.
type Deps struct {
	Jobs    JobRepository
	Clock   Clock
	Sleeper Sleeper
	Jitter  Jitter       // optional: nil means no jitter
	Log     *slog.Logger // optional: nil discards

	// Power and Settings drive the power-aware policy (PRD section 7.4). Both are optional: without a monitor the
	// power source is unknown, which counts as AC power; without settings the default "only on AC power" applies.
	Power    PowerMonitor
	Settings SettingsReader

	// Observer is told about every finished job (PRD section 15); optional: nil means nobody is told.
	Observer Observer
}

// Config tunes the worker pool.
type Config struct {
	Workers        int           // goroutines that run jobs, at least 1
	PollInterval   time.Duration // wait between polls of an idle worker
	DefaultTimeout time.Duration // limit of one attempt for a type without its own
	Backoff        backoff.Spec  // retry delays and the cap on attempts

	// Rechecks says when a job waiting for the network or the quota is tried again; the zero value means
	// backoff.DefaultRechecks.
	Rechecks backoff.Rechecks

	// MaxHeavy caps the jobs of Heavy types that run at the same time, so two workers never run two CPU-bound
	// transcriptions at once (PRD section 7.4); 1..Workers, the zero value means DefaultMaxHeavy.
	MaxHeavy int
}

// DefaultMaxHeavy is one heavy job at a time.
const DefaultMaxHeavy = 1

// DefaultConfig returns the settings for a desktop with one background class recording.
func DefaultConfig() Config {
	return Config{
		Workers:        2,
		PollInterval:   time.Second,
		DefaultTimeout: 30 * time.Minute,
		Backoff:        backoff.DefaultSpec(),
		Rechecks:       backoff.DefaultRechecks(),
		MaxHeavy:       DefaultMaxHeavy,
	}
}

// Registration binds a job type to its handler.
type Registration struct {
	Type    job.Type
	Handler Handler

	// Timeout limits one attempt; zero means Config.DefaultTimeout.
	Timeout time.Duration

	// Heavy marks local CPU-bound work such as STT: on battery with "process only on AC power" on, its jobs wait in
	// WAITING_FOR_POWER, and a running one stops at its next progress report (PRD section 7.4, AC-21).
	Heavy bool

	// Uncancellable marks a handler that is not safe to interrupt: a user cancellation of its running job is
	// refused with ErrNotCancellable (a queued or waiting job can still be cancelled).
	Uncancellable bool

	// Recovery says what happens to a job of this type that was interrupted: left RUNNING by a crash (Recover) or
	// stopped by a shutdown. It is required, so that no type is rerun by default (hard rule 7).
	Recovery Recovery

	// Reconciler decides the fate of an interrupted job of a RecoveryReconcile type. It is allowed only with that
	// policy; without it such a job fails instead of running again.
	//
	// Only interruptions reach it. ANY transient error or timeout of an attempt, even after a possible side effect,
	// goes through Retry; the runner does not enforce anything here. When the outcome of the send is ambiguous the
	// Handler MUST return backoff.Permanent or record the unknown outcome in its own aggregate (for a delivery:
	// UNKNOWN), so the retry does not resend. TASK-044 must have this as an acceptance criterion.
	Reconciler Reconciler
}

// Recovery is the policy for an interrupted job of one type (PRD section 7.2: resume or safely requeue by job type).
type Recovery int

// Recovery policies. The zero value is not a policy: Register refuses it.
const (
	recoveryUnset Recovery = iota

	// RecoveryRequeue is for idempotent work: the job goes back to QUEUED and the crashed attempt counts, so a job
	// without attempts left fails with "attempts exhausted".
	RecoveryRequeue

	// RecoveryReconcile is for work with external side effects (hard rule 7), such as email delivery: the job is never
	// requeued blindly. A shutdown leaves it RUNNING; Recover asks the Reconciler of the type, or fails the job when
	// there is none.
	RecoveryReconcile

	// RecoveryFail is for work that must not run again after an interruption and has nothing to reconcile: a shutdown
	// leaves the job RUNNING and Recover fails it.
	RecoveryFail
)

// valid reports whether r is one of the policies.
func (r Recovery) valid() bool { return r > recoveryUnset && r <= RecoveryFail }

// RecoveryReport is the summary of one Recover call. Every stale job is in exactly one list.
type RecoveryReport struct {
	Requeued  []shared.JobID // back to QUEUED, the crashed attempt counted
	Completed []shared.JobID // the reconciler confirmed the work was done
	Failed    []shared.JobID // attempts exhausted, RecoveryFail, no reconciler, or reconciled as failed

	Unregistered []shared.JobID  // no registration for the type: left RUNNING for a build that knows it
	Corrupt      []shared.JobID  // rows that cannot be restored: reported, not changed
	Conflicts    []shared.JobID  // changed elsewhere between the read and the write: skipped
	Errors       []RecoveryError // left RUNNING; the next Recover tries again
}

// RecoveryError is a stale job whose recovery failed.
type RecoveryError struct {
	Job  shared.JobID
	Type job.Type
	Err  error
}
