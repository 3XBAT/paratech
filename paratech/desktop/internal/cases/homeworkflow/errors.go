package homeworkflow

import (
	"errors"

	"paratech/desktop/internal/domain/coursework/values"
)

// Errors of the homework workflow use case (ARCH-18). Domain errors (proposal.ErrMalformed,
// proposal.ErrNotConfirmed, proposal.ErrInvalidTransition, ...) are returned wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid homework workflow configuration")

	// ErrUnsupportedKind means a stored proposal has a kind this use case cannot execute.
	ErrUnsupportedKind = errors.New("unsupported proposal kind")

	// ErrCalendarNotConnected is returned by a DeadlineCalendar that is not connected yet; the proposal stays
	// CONFIRMED and can be executed once it is.
	ErrCalendarNotConnected = errors.New("calendar not connected")

	// ErrGenerateRetries is returned by Drafts.Generate when the same generation (assignment, base version and
	// instructions) already failed or was cancelled too many times; other instructions or an edit start a new one.
	ErrGenerateRetries = errors.New("homework draft: the same generation failed too many times")

	// ErrNotASend is returned by Service.ConfirmSend for a proposal that is not a send_homework_email one.
	ErrNotASend = errors.New("homework workflow: not a send proposal")

	// ErrExportLocked is returned by Drafts.ExportDraft while an email of the assignment is unresolved (PENDING,
	// SENDING or UNKNOWN): the export file is its attachment, and overwriting it would fail or change that email.
	ErrExportLocked = errors.New("homework draft: the export file is the attachment of an unresolved email")

	// ErrNoPath is returned by Drafts.SaveDocx without a target path.
	ErrNoPath = errors.New("homework draft: no file to save to")

	// ErrGenerationRunning is returned by Drafts.Generate while another generation of the assignment is pending or
	// running (TASK-094): one draft job per assignment at a time. The same request again returns that job instead.
	ErrGenerationRunning = errors.New("homework draft: a generation of the assignment is already running")
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
	ErrNothingToProcess = errors.New(values.NothingToProcessMessage)

	// ErrContextRefused means the lesson context cannot be sent: too large or not valid text. Permanent.
	ErrContextRefused = errors.New("post-processing: the lesson context cannot be sent")

	// ErrLLM means the LLM call failed. The cause decides: no network or no quota makes the job wait, a permanent
	// cause fails it, anything else is retried.
	ErrLLM = errors.New(values.LLMFailedMessage)

	// ErrInvalidAnswer means the model answers did not match the schema of the operation, also after the repeats with
	// a format reminder; nothing is stored. Permanent: a rerun of the job gets the same stored answers from the Hub
	// (idempotency keys); the user retries with a new job (Rerunner).
	ErrInvalidAnswer = errors.New("post-processing: the LLM answer does not match the schema")

	// ErrTruncatedAnswer is ErrInvalidAnswer for a last answer that looks cut off at the output limit of the model alias
	// (the desktop sends no limit of its own); the Hub owner may raise it. Permanent.
	ErrTruncatedAnswer = errors.New("post-processing: the LLM answer looks cut off at the output limit")

	// ErrStorage means reading or writing local state failed. Retried unless the cause is permanent.
	ErrStorage = errors.New("post-processing: local storage failed")

	// ErrStopped means the job runner asked the handler to stop (the job was cancelled or changed elsewhere).
	ErrStopped = errors.New("post-processing: stopped")

	// ErrEnqueue means the post-processing jobs of a transcript could not be created; the caller retries.
	ErrEnqueue = errors.New("post-processing: jobs could not be created")

	// ErrUnknownWork means Rerunner.Retry got a work that is not post-processing.
	ErrUnknownWork = errors.New("post-processing: unknown work")
)

// Errors of the homework_draft job (Drafts.Handle). They are stored as the job's last error and shown in the UI, so
// like the post-processing errors they are fixed texts; the cause goes to the log only. The other Drafts methods
// return domain errors (homeworkdraft.ErrStaleBase, values.ErrInvalidDraftRequest, shared.ErrNotFound, ...) wrapped.
var (
	// ErrDraftPayload means the job payload is not a valid draft request or the job is not a draft job. Permanent.
	ErrDraftPayload = errors.New("homework draft: invalid job")

	// ErrDraftAssignmentGone means the assignment of the draft no longer exists. Permanent.
	ErrDraftAssignmentGone = errors.New("homework draft: the assignment no longer exists")

	// ErrDraftChanged means the draft got a new version (a user edit or another generation) after the job was
	// started; the answer is not added. Permanent: generating again starts from the new version.
	ErrDraftChanged = errors.New("homework draft: the draft changed during the generation")

	// ErrDraftFull means the draft has the maximum number of versions. Permanent.
	ErrDraftFull = errors.New("homework draft: the draft has the maximum number of versions")

	// ErrDraftContextRefused means the assignment context cannot be sent: too large or not valid text. Permanent.
	ErrDraftContextRefused = errors.New("homework draft: the assignment context cannot be sent")

	// ErrDraftLLM means the LLM call failed. As for ErrLLM, the cause decides between waiting, failing and retrying.
	ErrDraftLLM = errors.New("homework draft: the LLM call failed")

	// ErrDraftInvalidAnswer means the model answer is not a valid draft; nothing is added. Permanent.
	ErrDraftInvalidAnswer = errors.New("homework draft: the LLM answer does not match the schema")

	// ErrDraftStorage means reading or writing local state failed. Retried unless the cause is permanent.
	ErrDraftStorage = errors.New("homework draft: local storage failed")

	// ErrDraftStopped means the job runner asked the handler to stop (the job was cancelled or changed elsewhere).
	ErrDraftStopped = errors.New("homework draft: stopped")
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
