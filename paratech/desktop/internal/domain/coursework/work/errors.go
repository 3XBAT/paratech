package work

import "errors"

// Errors of the aggregate (ARCH-18). None of them quotes the assignment, the material or the reader's answer.
var (
	// ErrInvalidWork means a work or its stored state breaks an invariant.
	ErrInvalidWork = errors.New("invalid work")

	// ErrInvalidTransition means the move is not allowed in the work's status.
	ErrInvalidTransition = errors.New("work: transition not allowed")

	// ErrInvalidRequest means the reader request cannot be built from the texts.
	ErrInvalidRequest = errors.New("invalid triage_assignment request")

	// ErrInvalidAnswer means the reader's answer breaks the schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid triage_assignment answer")

	// ErrTooManyRounds means an answer asked questions after the second round: there is no third question round.
	ErrTooManyRounds = errors.New("work: no third question round")

	// ErrStaleRound means an answer or a refusal is for another round than the one the work waits for.
	ErrStaleRound = errors.New("work: answer of another round")

	// ErrInvalidReply means a student's answer, edit or question does not fit the question or the field.
	ErrInvalidReply = errors.New("work: invalid answer")
)
