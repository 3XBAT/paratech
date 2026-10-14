package transcribe

import "errors"

// Errors of the STT job handler (ARCH-18). They are stored as the job's last error and shown in the UI, so they are
// short fixed texts without paths, ids or transcript text; the cause stays reachable through errors.Is and goes to
// the log only.
var (
	// ErrInvalidConfig is returned by New when a required port is missing.
	ErrInvalidConfig = errors.New("transcribe: invalid configuration")

	// ErrInvalidPayload means the job payload is not a valid STT request. Permanent.
	ErrInvalidPayload = errors.New("stt job: invalid payload")

	// ErrNoPinnedModel means the job pins no model (AC-08). Permanent.
	ErrNoPinnedModel = errors.New("stt job: no pinned model")

	// ErrRecordingNotFound means the recording no longer exists. Permanent.
	ErrRecordingNotFound = errors.New("stt job: recording not found")

	// ErrRecordingNotReady means the recording is not COMPLETED, so it has no final audio. Permanent.
	ErrRecordingNotReady = errors.New("stt job: recording is not completed")

	// ErrModelNotInstalled means the pinned model is no longer installed (PRD section 14 case 11). Permanent.
	ErrModelNotInstalled = errors.New("stt job: pinned model is not installed")

	// ErrInputMissing means the audio or the model file is missing on disk. Permanent.
	ErrInputMissing = errors.New("stt job: audio or model file is missing")

	// ErrTranscriptionFailed means the STT engine failed. Retried unless the cause is permanent.
	ErrTranscriptionFailed = errors.New("stt job: transcription failed")

	// ErrInvalidDocument means the engine reported success but the stored document is unreadable or invalid. Permanent.
	ErrInvalidDocument = errors.New("stt job: transcript document is invalid")

	// ErrInvalidTranscript means the transcript aggregate refused the result. Permanent.
	ErrInvalidTranscript = errors.New("stt job: transcript is invalid")

	// ErrTranscriptConflict means a stored transcript with the job's id belongs to another recording. Permanent.
	ErrTranscriptConflict = errors.New("stt job: stored transcript belongs to another recording")

	// ErrStorage means reading or writing local state failed. Retried unless the cause is permanent.
	ErrStorage = errors.New("stt job: local storage failed")

	// ErrPublish means the completion event could not be handed over. Retried.
	ErrPublish = errors.New("stt job: completion event failed")

	// ErrStopped means the job runner asked the handler to stop at a checkpoint (power loss, cancellation, a change
	// of the job elsewhere). The runner's error is the cause, so errors.Is finds powerpolicy.ErrPowerLost.
	ErrStopped = errors.New("stt job: stopped at a checkpoint")
)

// jobError has a fixed message and keeps the cause for errors.Is without putting its text into the message.
type jobError struct {
	msg   error
	cause error
}

func (e *jobError) Error() string   { return e.msg.Error() }
func (e *jobError) Unwrap() []error { return []error{e.msg, e.cause} }

// fail builds a jobError; a nil cause gives msg alone.
func fail(msg, cause error) error {
	if cause == nil {
		return msg
	}
	return &jobError{msg: msg, cause: cause}
}
