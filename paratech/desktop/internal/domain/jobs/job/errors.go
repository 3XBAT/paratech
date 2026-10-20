package job

import "errors"

// Errors of the job package (ARCH-18).
var (
	// ErrInvalidClaimFilter is returned by NewClaimFilter for an unusable filter.
	ErrInvalidClaimFilter = errors.New("invalid claim filter")

	// ErrInvalidProgress is returned by NewProgress for a percentage outside 0..100 or a malformed checkpoint.
	ErrInvalidProgress = errors.New("invalid job progress")

	// ErrInvalidJob is returned by New and Restore for an inconsistent job.
	ErrInvalidJob = errors.New("invalid job")

	// ErrInvalidTransition means the status table does not allow the requested change.
	ErrInvalidTransition = errors.New("invalid job status transition")

	// ErrNotDue means a QUEUED job cannot be claimed, or a RETRY_WAIT job requeued, before its next run time.
	ErrNotDue = errors.New("job is not due yet")

	// ErrAttemptsExhausted means a job has no attempt left to start.
	ErrAttemptsExhausted = errors.New("no attempts left")

	// ErrStillActive means a job cannot be rerun: it is still pending, waiting or running.
	ErrStillActive = errors.New("job is still active")

	// ErrNotFailed means a job cannot be retried: it completed.
	ErrNotFailed = errors.New("job did not fail")

	// ErrContinueLater is what a handler returns after one step of a job that does its work in steps (TASK-097):
	// the step is stored and the job goes to the end of the queue for the next one (Yield). It is a marker for the
	// runner, never a failure; handlers live in other use cases, so it is part of the domain.
	ErrContinueLater = errors.New("job continues later")
)
