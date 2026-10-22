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
)
