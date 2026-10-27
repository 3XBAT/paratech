package agentchats

import "errors"

// Errors of the use case. Domain errors (thread.ErrInvalidTitle, thread.ErrInvalidTransition) and shared.ErrNotFound
// pass through wrapped.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("agentchats: invalid configuration")

	// ErrInvalidInput means a chat ID is malformed.
	ErrInvalidInput = errors.New("agentchats: invalid input")

	// ErrBusy means the chat's request is running: its step loop owns the chat until the request waits or ends.
	ErrBusy = errors.New("agentchats: the chat's request is running")
)
