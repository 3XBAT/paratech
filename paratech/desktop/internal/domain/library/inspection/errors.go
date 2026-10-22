package inspection

import "errors"

// Errors of the inspection (ARCH-18). None of them quotes the material, a name or the model's answer.
var (
	// ErrInvalidRequest means a request that breaks the reader's input rules (no text, a bad candidate id or date).
	ErrInvalidRequest = errors.New("invalid inspect request")

	// ErrInvalidAnswer means the reader's answer breaks the schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid inspect answer")

	// ErrInvalidInspection means a state that breaks an invariant of the aggregate.
	ErrInvalidInspection = errors.New("invalid inspection")

	// ErrRefused means the reader refused the text MaxAttempts times; it is not asked again for that text.
	ErrRefused = errors.New("inspection refused too often")
)
