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

	// ErrDeleting means the recording is already marked for deletion (TASK-095).
	ErrDeleting = errors.New("recording is already being deleted")

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

	// ErrUnsupportedAudio means the content of an imported file is none of the formats whisper-cli decodes itself.
	ErrUnsupportedAudio = errors.New("the audio format is not supported")

	// ErrEmptyAudio means an imported file is empty.
	ErrEmptyAudio = errors.New("the audio file is empty")

	// ErrAudioTooLarge means an imported file is larger than MaxImportedBytes.
	ErrAudioTooLarge = errors.New("the audio file is too large")

	// ErrAudioTooLong means the header of an imported file says it lasts longer than MaxImportedDuration.
	ErrAudioTooLong = errors.New("the audio file is too long")

	// ErrNoDiskSpace means the copy of an imported file would leave too little free disk space.
	ErrNoDiskSpace = errors.New("not enough free disk space for the audio file")

	// ErrFutureLecture means an imported recording names a lecture that has not begun yet. NewImported wraps it
	// together with ErrInvalidRecording.
	ErrFutureLecture = errors.New("the lecture has not begun yet")
)
