package recordingprompt

import "errors"

// Errors of the prompt scenario (ARCH-18). A refused action on the current prompt wraps prompt.ErrInvalidTransition.
var (
	// ErrInvalidConfig is returned by New for missing ports or an unusable poll interval.
	ErrInvalidConfig = errors.New("invalid recording prompt configuration")

	// ErrNoPrompt means the action names a prompt that is not current: it ended or a newer one replaced it.
	ErrNoPrompt = errors.New("the recording prompt is no longer current")

	// ErrStartFailed means the recording did not start; the prompt (or the Manual result) carries its problem.
	ErrStartFailed = errors.New("the recording did not start")

	// ErrNotReady means recordings cannot start yet: interrupted recordings are being recovered.
	ErrNotReady = errors.New("recordings are not ready: interrupted recordings are being recovered")
)
