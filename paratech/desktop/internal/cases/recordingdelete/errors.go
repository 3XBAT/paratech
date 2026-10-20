package recordingdelete

import "errors"

// Errors of the recording deletion (ARCH-18). Fixed texts; causes are wrapped.
var (
	// ErrInvalidConfig is returned by New when a required port is missing or a setting is unusable.
	ErrInvalidConfig = errors.New("recordingdelete: invalid configuration")

	// ErrInvalidRequest means a malformed recording id, a chosen assignment not found in the recording, or too many
	// recordings at once.
	ErrInvalidRequest = errors.New("recordingdelete: invalid request")

	// ErrNotFound means the recording does not exist or is already being deleted.
	ErrNotFound = errors.New("recordingdelete: recording not found")

	// ErrInProgress means the recording is being captured or finalized: it is stopped first.
	ErrInProgress = errors.New("recordingdelete: the recording is in progress")

	// ErrRecovering means the startup recovery is assembling the recording right now; try again in a moment.
	ErrRecovering = errors.New("recordingdelete: the recording is being recovered")

	// ErrStorage means local storage could not be read or written.
	ErrStorage = errors.New("recordingdelete: local storage failed")
)

// errBusy means a job of the recording is still unfinished when the rows are to be removed.
var errBusy = errors.New("recordingdelete: a job of the recording is unfinished")
