package homeworkflow

import "errors"

// Errors of the homework workflow use case (ARCH-18). Domain errors (proposal.ErrMalformed,
// proposal.ErrNotConfirmed, proposal.ErrInvalidTransition, ...) are returned wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid homework workflow configuration")

	// ErrUnsupportedKind means a stored proposal has a kind this use case cannot execute.
	ErrUnsupportedKind = errors.New("unsupported proposal kind")
)
