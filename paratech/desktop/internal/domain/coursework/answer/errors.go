package answer

import "errors"

// Errors of the aggregate (ARCH-18). None of them quotes the question, a fragment or the reader's output.
var (
	// ErrInvalidQuestion means the question is blank, too long or not one line of text.
	ErrInvalidQuestion = errors.New("invalid question")

	// ErrInvalidRequest means the fragments break the reader's input bounds.
	ErrInvalidRequest = errors.New("invalid answer request")

	// ErrInvalidAnswer means the reader's output breaks the schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid answer reader output")

	// ErrInvalidState means stored or given state breaks an invariant of the aggregate.
	ErrInvalidState = errors.New("invalid answer state")
)
