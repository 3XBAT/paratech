package subjectview

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/services/today"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// SubjectReader loads a subject of the library. Implemented by the sqlite adapter.
type SubjectReader interface {
	// Get loads a subject; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.SubjectID) (*subject.Subject, error)
}

// MaterialList lists the files of a subject. Implemented by the sqlite adapter.
type MaterialList interface {
	// ListIn returns the files of a subject in any status, newest first.
	ListIn(ctx context.Context, sub shared.SubjectID) ([]*material.Material, error)
}

// SubjectResolver maps free subject strings to the subjects of the library (ADR-0024): name or alias, an ambiguous
// string matches none, unmatched strings are absent. Implemented by cases/library, wired by the app.
type SubjectResolver interface {
	ResolveSubjects(ctx context.Context, names []string) (map[string]subjectmatch.Match, error)
}

// Assignments gives the assignments as the homework screen shows them: title, subject string and deadline (PRD
// section 10). The app implements it over the homework use case; mapping only.
type Assignments interface {
	// Deadlines returns the listed assignments with their deadlines.
	Deadlines(ctx context.Context) ([]today.Deadline, error)
}

// LessonReader reads the imported schedule (PRD section 11). Implemented by the sqlite adapter.
type LessonReader interface {
	// ListStartingBetween returns the live lessons starting in [from, to), ordered by start.
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)
}

// KnowledgeBase lists the recorded lessons of a subject as the knowledge base groups them (TASK-091, ADR-0024).
// Implemented by cases/knowledgebase, wired by the app.
type KnowledgeBase interface {
	// Lessons returns the lessons of the group with key (kb.LibraryKey of a library subject), newest first.
	Lessons(ctx context.Context, key string) ([]kb.Lesson, error)
}

// SummaryReader reads the summary of a transcript, for the time it appeared. Implemented by the sqlite adapter.
type SummaryReader interface {
	// Get loads a summary; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)
}

// Failures tells which recordings stopped on a failed processing step (TASK-076 chain status). The app implements it
// over the activity use case; mapping only.
type Failures interface {
	// FailedRecordings returns the recordings whose processing chain failed.
	FailedRecordings(ctx context.Context) ([]shared.RecordingID, error)
}

// Visits keeps the time the student last opened the page of each subject, locally. Implemented by the visitfile
// adapter.
type Visits interface {
	// LastOpened returns the last opening of a subject's page; zero when it was never opened.
	LastOpened(ctx context.Context, id shared.SubjectID) (time.Time, error)

	// MarkOpened records an opening of a subject's page at at.
	MarkOpened(ctx context.Context, id shared.SubjectID, at time.Time) error
}

// Clock supplies the current time in the student's zone, whose calendar days and weekdays the page uses (ARCH-24).
type Clock interface {
	Now() time.Time
}
