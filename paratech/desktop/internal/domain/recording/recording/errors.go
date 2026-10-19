package recording

import "errors"

// Errors of the recording package (ARCH-18).
var (
	// ErrInvalidRecording is returned by New, Restore and the methods that take data for an inconsistent recording.
	ErrInvalidRecording = errors.New("invalid recording")

	// ErrInvalidTransition means the status table does not allow the requested change.
	ErrInvalidTransition = errors.New("invalid recording status transition")

	// ErrLimitNotReached means the hard cap was asked to finalize a recording before its deadline.
	ErrLimitNotReached = errors.New("recording limit not reached")

	// ErrNotDeletable means the recording is still being captured or finalized and cannot be deleted.
	ErrNotDeletable = errors.New("recording cannot be deleted while it is in progress")

	// ErrNoAudio means the chunk directory of a recording holds no readable audio: no chunk at all, or only chunks
	// that cannot be read. It is returned by the Recorder adapters, so the recording fails instead of being retried.
	ErrNoAudio = errors.New("recording has no readable audio")

	// ErrCaptureUnavailable means system audio cannot be captured at all in this build, so a start fails at once and
	// a retry does not help. Recorder adapters wrap it, so a use case can tell it apart without importing them.
	ErrCaptureUnavailable = errors.New("system audio capture is not available")

	// ErrSourceNotRunning means the app chosen as the recording source does not run, so its audio cannot be captured.
	// Recorder adapters wrap it.
	ErrSourceNotRunning = errors.New("the chosen app is not running")

	// ErrSourceDidNotStart means the capture of the source a running recording was switched to did not start, so the
	// capture went back to its old source (TASK-082). Recorder adapters wrap it.
	ErrSourceDidNotStart = errors.New("the capture of the chosen source did not start")
)
