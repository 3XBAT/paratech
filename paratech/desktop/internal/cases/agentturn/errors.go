package agentturn

import "errors"

// Errors of the use case. Domain errors (thread.ErrTurnInProgress, thread.ErrInvalidTransition,
// thread.ErrRetryRequired, thread.ErrRestartRequired, thread.ErrBudgetExceeded, thread.ErrRerunBlocked) pass through
// wrapped.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("agentturn: invalid configuration")

	// ErrInvalidInput means the request text, a scope or an answer breaks its rules.
	ErrInvalidInput = errors.New("agentturn: invalid input")

	// ErrStaleTurn means the button belongs to an older request or the thread does not exist.
	ErrStaleTurn = errors.New("agentturn: the request is not current")
)
