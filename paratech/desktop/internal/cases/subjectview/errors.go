package subjectview

import "errors"

// Errors of the subject page (ARCH-18). Fixed texts without names; causes are wrapped for the log.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("subjectview: invalid configuration")

	// ErrInvalidRequest means a malformed subject id.
	ErrInvalidRequest = errors.New("subjectview: invalid request")

	// ErrSubjectNotFound means there is no such subject (any more).
	ErrSubjectNotFound = errors.New("subjectview: subject not found")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("subjectview: local storage failed")
)
