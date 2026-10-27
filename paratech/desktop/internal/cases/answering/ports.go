package answering

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/shared"
)

// Knowledge gives the lessons with their texts (ADR-0019: read at query time). Implemented by cases/knowledgebase,
// wired by the app.
type Knowledge interface {
	// Texts returns the lessons of a library subject with their texts, newest first; a zero subject every lesson.
	Texts(ctx context.Context, subject shared.SubjectID) ([]kb.LessonText, error)
}

// Reader runs the quarantined reader ANSWER on the Hub. Implemented by the hubclient adapter. The output is untrusted
// model text; the only thing done with it is answer.Request.Parse.
type Reader interface {
	// AnswerQuestion sends req under its key. A permanent refusal of the answer (the Hub's schema check, charged, or
	// the provider's) wraps answer.ErrInvalidAnswer; a used-up quota shared.ErrQuotaExceeded; anything else means the
	// Hub was not reached or refused the request, and nothing was charged as an answer. A non-nil partial gets the
	// output received so far, growing, in order, on the calling goroutine and before AnswerQuestion returns; it is
	// untrusted draft text and the returned output may differ from the last one (ADR-0026).
	AnswerQuestion(ctx context.Context, req answer.Request, partial func(soFar string)) (string, error)
}

// DraftSink shows the draft of an answer being written (ADR-0026). Implemented by input/agent. Draft gets the claim
// texts written so far (answer.Draft), only when they change; they are untrusted model text, for display as a draft
// only. It is called on the goroutine of Ask and must not block.
type DraftSink interface {
	Draft(texts []string)
}

// Repository stores the answers. Implemented by the sqlite adapter.
type Repository interface {
	// Get loads an answer; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.AnswerID) (*answer.Answer, error)

	// FindByKey loads the answer of a request key; none wraps shared.ErrNotFound.
	FindByKey(ctx context.Context, key string) (*answer.Answer, error)

	// Insert stores a new answer; an answer of the same key wraps shared.ErrDuplicate.
	Insert(ctx context.Context, a *answer.Answer) error
}

// Notes tells whether a note of the student is still there, so a citation of a removed one shows as removed
// (ADR-0027). Implemented by the app over the note repository.
type Notes interface {
	// NoteActive reports whether the note exists and is not removed; a note gone is false, not an error.
	NoteActive(ctx context.Context, id shared.NoteID) (bool, error)
}

// Clock supplies the current time (ARCH-24).
type Clock interface {
	Now() time.Time
}

// IDs generates answer identifiers (ARCH-24).
type IDs interface {
	NewAnswerID() shared.AnswerID
}
