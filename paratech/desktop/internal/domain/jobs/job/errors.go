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
)
