package answering

import (
	"log/slog"

	"paratech/desktop/internal/domain/coursework/answer"
)

// Deps are the ports of the use case; all but Log are required.
type Deps struct {
	Knowledge Knowledge
	Reader    Reader
	Answers   Repository
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
