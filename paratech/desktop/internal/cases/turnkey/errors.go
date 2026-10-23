package turnkey

import "errors"

// Errors of the use case. Each wraps its cause; none quotes the assignment or the reader's answer.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("turnkey: invalid configuration")

	// ErrNotFound means the work does not exist (also shared.ErrNotFound).
	ErrNotFound = errors.New("turnkey: work not found")

	// ErrSourceNotFound means the assignment or the file of the work is gone (or the file was removed).
	ErrSourceNotFound = errors.New("turnkey: the assignment is gone")

	// ErrNoText means the source has no text the reader can read (a PDF, an unreadable or an empty file).
	ErrNoText = errors.New("turnkey: the assignment has no readable text")

	// ErrInvalidRequest means the move does not fit the work (its status, a question, an answer or an edit).
	ErrInvalidRequest = errors.New("turnkey: invalid request")

	// ErrReaderUnavailable means the reader could not be asked (no network, no Hub); the same call is free later.
	ErrReaderUnavailable = errors.New("turnkey: the reader is unavailable")

	// ErrQuota means the quota is used up.
	ErrQuota = errors.New("turnkey: quota exceeded")

	// ErrReaderRefused means the reader's answer broke the schema: the attempt is used (charged); after the last
	// attempt the work fails.
	ErrReaderRefused = errors.New("turnkey: the reader's answer was refused")

	// ErrChanged means the work changed since it was read (Undo of a work the student has answered or edited).
	ErrChanged = errors.New("turnkey: the work changed")

	// ErrNotConfigured means «Спросить по шагу» has no advisor yet.
	ErrNotConfigured = errors.New("turnkey: questions about a step are not available")

	// ErrUnavailable means local storage could not be read or written.
	ErrUnavailable = errors.New("turnkey: local storage failed")

	// ErrNotWritable means the work cannot be written (yet): its requirements are not confirmed, or it is a
	// presentation.
	ErrNotWritable = errors.New("turnkey: the work cannot be written")

	// ErrAlreadyWritten means the work is done: its file is ready; Regenerate or EditOutline writes it again.
	ErrAlreadyWritten = errors.New("turnkey: the work is written already")

	// ErrWritingRunning means a writing job of the work runs: the outline cannot change meanwhile.
	ErrWritingRunning = errors.New("turnkey: the work is being written")

	// ErrWritingRetries means the writing was started again too often after failed or cancelled jobs.
	ErrWritingRetries = errors.New("turnkey: the writing was retried too often")
)

// The fixed errors a work_writing job ends with: the job keeps only the text of its last error (never a cause, a
// text of the work or of the model).
var (
	ErrWritePayload = errors.New("turnkey writing: invalid job payload")
	ErrWriteGone    = errors.New("turnkey writing: the work or its assignment is gone")
	ErrWriteLLM     = errors.New("turnkey writing: the model call failed")
	ErrWriteAnswer  = errors.New("turnkey writing: the model answer was refused")
	ErrWriteBlocked = errors.New("turnkey writing: every attempt of a step was refused")
	ErrWriteFile    = errors.New("turnkey writing: the file could not be built")
	ErrWriteStorage = errors.New("turnkey writing: local storage failed")
	ErrWriteStopped = errors.New("turnkey writing: stopped")
)

// jobError is a fixed error of a job with its cause, for errors.Is (the runner reads the network and quota causes);
// its text is the fixed one only.
type jobError struct {
	msg   error
	cause error
}

func (e *jobError) Error() string   { return e.msg.Error() }
func (e *jobError) Unwrap() []error { return []error{e.msg, e.cause} }

func fail(msg, cause error) error {
	if cause == nil {
		return msg
	}
	return &jobError{msg: msg, cause: cause}
}
