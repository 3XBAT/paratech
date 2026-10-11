package job

import "errors"

// Errors of the value objects of the job package (ARCH-18). Transition errors arrive with TASK-008.
var (
	// ErrInvalidClaimFilter is returned by NewClaimFilter for an unusable filter.
	ErrInvalidClaimFilter = errors.New("invalid claim filter")

	// ErrInvalidProgress is returned by NewProgress for a percentage outside 0..100 or a malformed checkpoint.
	ErrInvalidProgress = errors.New("invalid job progress")
)
