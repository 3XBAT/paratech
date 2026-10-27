package recordingimport

import "errors"

// Errors of the import (ARCH-18). Fixed texts; causes are wrapped. The refusals of the file itself are the domain's:
// recording.ErrEmptyAudio, ErrAudioTooLarge, ErrAudioTooLong, ErrUnsupportedAudio, ErrNoDiskSpace and ErrFutureLecture.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("recordingimport: invalid configuration")

	// ErrInvalidRequest means a malformed lesson id or no lecture time without a lesson.
	ErrInvalidRequest = errors.New("recordingimport: invalid request")

	// ErrLessonNotFound means the chosen lesson does not exist (any more).
	ErrLessonNotFound = errors.New("recordingimport: lesson not found")

	// ErrUnreadable means the chosen file cannot be opened or read: missing, not a regular file, not on a local drive.
	ErrUnreadable = errors.New("recordingimport: the file cannot be read")

	// ErrChanged means the file changed size while it was copied.
	ErrChanged = errors.New("recordingimport: the file changed while it was copied")

	// ErrDeleting means the recording of the same file is being deleted right now; try again in a moment.
	ErrDeleting = errors.New("recordingimport: the recording of this file is being deleted")

	// ErrStorage means local storage could not be read or written.
	ErrStorage = errors.New("recordingimport: local storage failed")
)
