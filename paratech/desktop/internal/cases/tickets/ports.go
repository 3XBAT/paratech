package tickets

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/ticketset"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// Repository stores the ticket sets. Implemented by the sqlite adapter.
type Repository interface {
	// Get loads a set; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.TicketSetID) (*ticketset.Set, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict.
	Save(ctx context.Context, s *ticketset.Set) error
}

// MaterialTexts reads a library material and its text (cases/library MaterialText, wired by the app). Errors wrap the
// domain sentinels: shared.ErrNotFound, material.ErrRemoved, material.ErrTextNotExtracted (PDF), material.ErrDamaged,
// material.ErrTextUnreadable; anything else is a local failure.
type MaterialTexts interface {
	MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error)
}

// Answerer answers one question from the lessons of a subject with checked citations (cases/answering, a paid read on
// the strong model, wired by the app). The same question about the same fragments returns the stored answer free.
type Answerer interface {
	// Ask returns the answer; nil when no passage of the lessons matches (nothing paid). Errors: a refusal for good
	// wraps answer.ErrInvalidAnswer, a question the reader does not take answer.ErrInvalidQuestion, a used-up quota
	// shared.ErrQuotaExceeded, no connection shared.ErrNetworkUnavailable; anything else is a local failure.
	Ask(ctx context.Context, question string, sub shared.SubjectID) (*answer.Answer, error)

	// Answer loads a stored answer; none wraps shared.ErrNotFound.
	Answer(ctx context.Context, id shared.AnswerID) (*answer.Answer, error)
}

// Shelf is the library as the tickets use it (cases/library, wired by the app): the subjects, the filing of the file
// into the subject's «Билеты» (into «Входящие» when the subject is gone) and MoveToInbox, the Undo of that filing (the
// file kept).
type Shelf interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
	FileTickets(ctx context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error)
	MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error)
}

// Jobs creates and reads the ticket_answers jobs. Implemented by the sqlite job repository.
type Jobs interface {
	// Enqueue stores a new job; a job of the same key is returned instead with created false.
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
	// Get loads a job; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.JobID) (*job.Job, error)
	// LatestByKeyPrefix returns the newest job of typ whose key starts with prefix; none wraps shared.ErrNotFound.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)
}

// Canceller cancels a job (cases/jobrunner, wired by the app); a job that cannot be cancelled any more is an error the
// use case only logs.
type Canceller interface {
	Cancel(ctx context.Context, id shared.JobID) error
}

// Profiles reads the student profile for the title page. Implemented by the sqlite settings repository.
type Profiles interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// Renderer renders a document as a Word file; the same document gives the same bytes. Implemented by the docx adapter.
type Renderer interface {
	Render(ctx context.Context, doc cvalues.Document) ([]byte, error)
}

// Clock supplies the current time in the student's zone (ARCH-24): the year of the title page and the zone of the
// lesson dates are read from it.
type Clock interface {
	Now() time.Time
}

// IDs generates the identifiers of new sets and jobs (ARCH-24).
type IDs interface {
	NewTicketSetID() shared.TicketSetID
	NewJobID() shared.JobID
}
