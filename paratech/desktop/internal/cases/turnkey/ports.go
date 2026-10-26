package turnkey

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// WorkRepository stores the works. Implemented by the sqlite adapter.
type WorkRepository interface {
	// Get loads a work; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.WorkID) (*work.Work, error)

	// ActiveOf loads the work of a source that has not failed; none wraps shared.ErrNotFound.
	ActiveOf(ctx context.Context, src work.Source) (*work.Work, error)

	// Save writes it under its version: a stale one wraps shared.ErrVersionConflict, a second active work of the
	// same source shared.ErrDuplicate, a source that does not exist shared.ErrConflict.
	Save(ctx context.Context, w *work.Work) error

	// Delete removes it if it is still at version expected, else shared.ErrVersionConflict.
	Delete(ctx context.Context, id shared.WorkID, expected int64) error
}

// Assignments reads the assignment a work is made from. Implemented by the sqlite adapter.
type Assignments interface {
	// Get loads an assignment; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error)
}

// MaterialTexts reads the text of a library material (cases/library MaterialText, wired by the app). Errors wrap the
// domain sentinels: shared.ErrNotFound, material.ErrRemoved, material.ErrTextNotExtracted (PDF), material.ErrDamaged,
// material.ErrTextUnreadable; anything else is a local failure.
type MaterialTexts interface {
	MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error)
}

// Reader is the quarantined reader TRIAGE_ASSIGNMENT on the Hub (ADR-0022). Implemented by the hubclient adapter. The
// output is untrusted model text: only work.Request.Parse reads it. Errors follow the Hub client:
// shared.ErrQuotaExceeded; work.ErrInvalidAnswer for any permanent provider error (the Hub's charged schema refusal
// or an uncharged model refusal, both one used attempt); anything else (no network, no Hub, a refused key) uses no
// attempt, and the same key is asked again later for free.
type Reader interface {
	TriageAssignment(ctx context.Context, req work.Request) (string, error)
}

// StepAdvisor answers «Спросить по шагу» from the narrow context of one helper step (work.StepQuestion), with no
// tools. No implementation exists yet: the Hub has no operation for it (ADR-0025); without one AskStep answers
// ErrNotConfigured. The answer is untrusted text for a card only.
type StepAdvisor interface {
	AdviseStep(ctx context.Context, q work.StepQuestion) (string, error)
}

// Clock supplies the current time in the student's zone (ARCH-24); the year of a title page is read from it.
type Clock interface {
	Now() time.Time
}

// Writer is the strong model that writes a work (TASK-131), through the Hub's homework_draft operation. Implemented
// by the hubclient adapter. The output is untrusted: only the parsers of work (ParseOutline, ParseSection,
// ParseSlideOutline, ParseSlide, ParseQuestions) read it. Errors:
// shared.ErrNetworkUnavailable and shared.ErrQuotaExceeded let the job wait; others are retried or permanent.
type Writer interface {
	Complete(ctx context.Context, req cvalues.LLMRequest) (cvalues.LLMResult, error)
}

// Jobs creates and reads the work_writing jobs. Implemented by the sqlite job repository.
type Jobs interface {
	// Enqueue stores a new job; a job of the same key is returned instead with created false.
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
	// Get loads a job; none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.JobID) (*job.Job, error)
	// LatestByKeyPrefix returns the newest job of typ whose key starts with prefix; none wraps shared.ErrNotFound.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)
}

// JobCanceller cancels a job at the student's request (cases/jobrunner, wired by the app): a queued or waiting job at
// once, a running one once its handler returns. A job that cannot be cancelled (finished meanwhile, or RUNNING with no
// handler of this process) is an error.
type JobCanceller interface {
	Cancel(ctx context.Context, id shared.JobID) error
}

// Profiles reads the student profile for the title page (TASK-094). Implemented by the sqlite settings repository.
type Profiles interface {
	Load(ctx context.Context) (*settings.Settings, error)
}

// Renderer renders a paper as a Word file; the same document gives the same bytes. Implemented by the docx adapter.
type Renderer interface {
	Render(ctx context.Context, doc cvalues.Document) ([]byte, error)
}

// SlideRenderer renders a presentation as a PowerPoint file; the same presentation gives the same bytes. Implemented
// by the pptx adapter.
type SlideRenderer interface {
	Render(ctx context.Context, p cvalues.Presentation) ([]byte, error)
}

// LessonLabels tells what an assignment shows of its lesson: the subject and the teacher of the title page, from the
// snapshot its recording kept (TASK-088). Implemented by cases/homeworkflow (wired by the app). None of the assignment
// wraps shared.ErrNotFound.
type LessonLabels interface {
	LessonLabel(ctx context.Context, id shared.AssignmentID) (cvalues.LessonLabel, error)
}

// Shelf is the library as the writing uses it (cases/library, wired by the app): the subject of the work, the
// filing of the finished paper into its «ДЗ» (into «Входящие» without a subject or when the subject is gone), and
// MoveToInbox, which takes a paper the work replaced back to «Входящие» (the file kept).
type Shelf interface {
	Subjects(ctx context.Context) ([]*subject.Subject, error)
	ResolveSubjects(ctx context.Context, names []string) (map[string]subjectmatch.Match, error)
	FileHomework(ctx context.Context, sub shared.SubjectID, name string, data []byte) (*material.Material, error)
	MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error)
}

// IDs generates the identifiers of new works (ARCH-24).
type IDs interface {
	NewWorkID() shared.WorkID
}

// JobIDs generates the identifiers of new jobs (ARCH-24).
type JobIDs interface {
	NewJobID() shared.JobID
}
