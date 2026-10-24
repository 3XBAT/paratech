package answering

import "errors"

// Errors of the use case. None of them quotes the question, a lesson or the reader's answer.
var (
	// ErrInvalidConfig means a required port is missing.
	ErrInvalidConfig = errors.New("answering: invalid configuration")

	// ErrInvalidQuestion means the question is blank, longer than 500 characters or not one line.
	ErrInvalidQuestion = errors.New("answering: invalid question")

	// ErrNotFound means there is no answer with that ID.
	ErrNotFound = errors.New("answering: answer not found")

	// ErrNoConnection means the Hub was not reached, is not set up or refused the request: nothing was charged as an
	// answer, asking again later repeats the same key.
	ErrNoConnection = errors.New("answering: no connection to the Hub")

	// ErrQuota means the quota is used up.
	ErrQuota = errors.New("answering: quota exceeded")

	// ErrReaderRefused means the reader's answer broke the schema or the provider refused it for good. The same
	// question about the same fragments repeats the refusal free (same key); another wording is a new request.
	ErrReaderRefused = errors.New("answering: the reader's answer was refused")

	// ErrUnavailable means the local store could not be read or written.
	ErrUnavailable = errors.New("answering: local data unavailable")
)
