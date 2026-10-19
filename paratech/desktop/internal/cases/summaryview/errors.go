package summaryview

import "errors"

// Errors of the summary viewer (ARCH-18). Fixed texts without paths or summary text; causes go to the log.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("summaryview: invalid configuration")

	// ErrInvalidRequest means an invalid recording id, format or path.
	ErrInvalidRequest = errors.New("summaryview: invalid request")

	// ErrRecordingNotFound means the recording does not exist (any more).
	ErrRecordingNotFound = errors.New("summaryview: recording not found")

	// ErrNoSummary means there is nothing to copy or save yet: the recording has no summary.
	ErrNoSummary = errors.New("summaryview: no summary yet")

	// ErrUnavailable means local storage could not be read or the file could not be written.
	ErrUnavailable = errors.New("summaryview: local storage failed")
)
