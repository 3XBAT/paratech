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

// Errors of the post-processing jobs (PostProcessor). They are stored as the job's last error and shown in the UI, so
// they are short fixed texts without ids, transcript text or model output; the cause stays reachable through
// errors.Is (the network and quota waits, the retry class) and goes to the log only.
var (
	// ErrUnknownJobType means the job is not a post-processing job. Permanent.
	ErrUnknownJobType = errors.New("post-processing: unknown job type")

	// ErrInvalidPayload means the job payload is not a valid post-processing request. Permanent.
	ErrInvalidPayload = errors.New("post-processing: invalid payload")

	// ErrTranscriptNotFound means the transcript or its recording no longer exists. Permanent.
	ErrTranscriptNotFound = errors.New("post-processing: transcript not found")

	// ErrTranscriptUnreadable means the transcript document is missing or invalid. Permanent.
	ErrTranscriptUnreadable = errors.New("post-processing: transcript document is unreadable")

	// ErrNothingToProcess means the transcript holds no recognised speech. Permanent.
	ErrNothingToProcess = errors.New("post-processing: the transcript is empty")

	// ErrContextRefused means the lesson context cannot be sent: too large or not valid text. Permanent.
	ErrContextRefused = errors.New("post-processing: the lesson context cannot be sent")

	// ErrLLM means the LLM call failed. The cause decides: no network or no quota makes the job wait, a permanent
	// cause fails it, anything else is retried.
	ErrLLM = errors.New("post-processing: the LLM call failed")

	// ErrInvalidAnswer means the model answer does not match the schema of its operation; nothing is stored. Permanent:
	// the same request returns the same stored answer from the Hub (idempotency key).
	ErrInvalidAnswer = errors.New("post-processing: the LLM answer does not match the schema")

	// ErrStorage means reading or writing local state failed. Retried unless the cause is permanent.
	ErrStorage = errors.New("post-processing: local storage failed")

	// ErrStopped means the job runner asked the handler to stop (the job was cancelled or changed elsewhere).
	ErrStopped = errors.New("post-processing: stopped")

	// ErrEnqueue means the post-processing jobs of a transcript could not be created; the caller retries.
	ErrEnqueue = errors.New("post-processing: jobs could not be created")
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
