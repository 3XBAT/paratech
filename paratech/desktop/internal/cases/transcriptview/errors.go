package transcriptview

import "errors"

// Errors of the transcript viewer (ARCH-18). Fixed texts without paths or transcript text; causes go to the log.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("transcriptview: invalid configuration")

	// ErrInvalidRequest means an invalid recording id, format or page.
	ErrInvalidRequest = errors.New("transcriptview: invalid request")

	// ErrRecordingNotFound means the recording does not exist (any more).
	ErrRecordingNotFound = errors.New("transcriptview: recording not found")

	// ErrNoTranscript means there is nothing to copy or save yet: no transcript and no live parts.
	ErrNoTranscript = errors.New("transcriptview: nothing recognised yet")

	// ErrNoAudio means the recording has no final track to play.
	ErrNoAudio = errors.New("transcriptview: recording has no audio")

	// ErrUnavailable means local storage could not be read or the file could not be written.
	ErrUnavailable = errors.New("transcriptview: local storage failed")
)
