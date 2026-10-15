package prompt

import "errors"

// Errors of the prompt package (ARCH-18).
var (
	// ErrInvalidPrompt is returned by the constructors for inconsistent data.
	ErrInvalidPrompt = errors.New("invalid recording prompt")

	// ErrInvalidTransition means the state table does not allow the requested change, for example a second start
	// while one runs or an action on a finished prompt.
	ErrInvalidTransition = errors.New("invalid recording prompt transition")

	// ErrSnoozeNotAllowed means the prompt cannot be snoozed: it is not a lesson prompt, or the lesson ends before the
	// prompt would be shown again.
	ErrSnoozeNotAllowed = errors.New("recording prompt cannot be snoozed")

	// ErrNotDue means a time-driven change (show again, expire) was asked for before its time.
	ErrNotDue = errors.New("recording prompt change is not due")

	// ErrInvalidProblem means a failed start was reported without a known problem kind.
	ErrInvalidProblem = errors.New("invalid recording prompt problem")
)
