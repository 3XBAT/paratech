package answering

import (
	"log/slog"

	"paratech/desktop/internal/domain/coursework/answer"
)

// Deps are the ports of the use case; all but Notes and Log are required. Without Notes no citation shows as removed.
type Deps struct {
	Knowledge Knowledge
	Reader    Reader
	Answers   Repository
	Notes     Notes
	Clock     Clock
	IDs       IDs
	Log       *slog.Logger
}

// Asked is the outcome of a question. Answer is nil when no passage of the lessons matched it: nothing was asked and
// nothing was charged. Reused tells that the same question about the same fragments was answered before and the stored
// answer is returned.
type Asked struct {
	Answer *answer.Answer
	Reused bool
}

// Card is a stored answer for its card. Removed has one entry per Answer.Sources(): true for a source of the student's
// note removed since the answer was made. The answer itself is unchanged.
type Card struct {
	Answer  *answer.Answer
	Removed []bool
}
