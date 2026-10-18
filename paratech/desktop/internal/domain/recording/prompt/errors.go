package prompt

import "errors"

// Errors of the prompt package (ARCH-18).
var (
	// ErrInvalidPrompt is returned by the constructors for inconsistent data.
	ErrInvalidPrompt = errors.New("invalid recording prompt")

	// ErrInvalidTransition means the state table does not allow the requested change, for example a second start
	// while one runs or an action on a finished prompt.
	ErrInvalidTransition = errors.New("invalid recording prompt transition")

	// ErrNotDue means expiry was asked for before the lesson ended.
	ErrNotDue = errors.New("recording prompt change is not due")

	// ErrInvalidProblem means a failed start was reported without a known problem kind.
	ErrInvalidProblem = errors.New("invalid recording prompt problem")
)
