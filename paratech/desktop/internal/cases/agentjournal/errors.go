package agentjournal

import "errors"

// Errors of the use case. Domain errors (action.ErrInvalidAction, action.ErrInvalidTransition,
// action.ErrInvalidCompensation) and shared.ErrNotFound pass through wrapped.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("agentjournal: invalid configuration")

	// ErrUndoUnavailable means no compensator is wired yet; the entry stays UNDOING and is retried at the next start.
	ErrUndoUnavailable = errors.New("agentjournal: undo is not available")
)
