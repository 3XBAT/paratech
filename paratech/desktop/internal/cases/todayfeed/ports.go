package todayfeed

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/agent/post"
	"paratech/desktop/internal/domain/agent/services/today"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// PostRepository stores the posts after lessons (PRD section 9, TASK-125). Implemented by the sqlite adapter.
type PostRepository interface {
	// Get loads the post of a recording (PRD section 9); none wraps shared.ErrNotFound.
	Get(ctx context.Context, rec shared.RecordingID) (*post.Post, error)

	// Save inserts a new post (version 0) or updates a stored one with a compare-and-swap on its version (PRD section
	// 9, ADR-0004(d)). A second post of the same recording or a stale version is shared.ErrVersionConflict; a recording
	// that no longer exists wraps shared.ErrConflict.
	Save(ctx context.Context, p *post.Post) error

	// ListUpdatedSince returns the posts changed at or after since, newest first, at most limit (PRD section 9).
	ListUpdatedSince(ctx context.Context, since time.Time, limit int) ([]*post.Post, error)
}

// TranscriptReader finds the recording of a transcript and its newest transcript (PRD section 8). Implemented by the
// sqlite adapter.
type TranscriptReader interface {
	// Get loads a transcript (PRD section 8); none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error)

	// LatestByRecording loads the newest transcript of a recording (PRD section 8); none wraps shared.ErrNotFound.
	LatestByRecording(ctx context.Context, rec shared.RecordingID) (*transcript.Transcript, error)
}

// SummaryReader tells whether a transcript has a summary (PRD section 9): a transcript without speech never gets one
// (TASK-110). Implemented by the sqlite adapter.
type SummaryReader interface {
	// Get loads a summary (PRD section 9); none wraps shared.ErrNotFound.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)
}

// RecordingReader reads the recording of a post for its lesson (PRD section 6.2). Implemented by the sqlite adapter.
type RecordingReader interface {
	// Get loads a recording (PRD section 6.2); none wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// LessonReader reads the schedule (PRD section 11). Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson (PRD section 11); none or a removed one wraps shared.ErrNotFound.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)

	// ListStartingBetween returns the live lessons starting in [from, to), ordered by start (PRD section 11).
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)
}

// AssignmentReader lists the assignments found in a transcript (PRD section 10). Implemented by the sqlite adapter.
type AssignmentReader interface {
	// ListByTranscript returns the assignments extracted from transcript t (PRD section 10), deleted ones skipped
	// (ADR-0029); none is an empty list.
	ListByTranscript(ctx context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error)
}

// Assignments gives the assignments as the homework screen shows them: title, subject (correction, schedule or hint)
// and deadline (PRD section 10). The app implements it over the homework use case; mapping only.
type Assignments interface {
	// Deadlines returns the listed assignments with their deadlines (PRD section 10).
	Deadlines(ctx context.Context) ([]today.Deadline, error)

	// Label returns the title and the subject an assignment shows (PRD section 10); none wraps shared.ErrNotFound.
	Label(ctx context.Context, id shared.AssignmentID) (title, subject string, err error)
}

// ProposalList lists the action proposals in one status (PRD section 13.3): VALIDATED ones wait for the student's
// «Да». Implemented by the sqlite adapter.
type ProposalList interface {
	// ListByStatus returns the proposals in status, newest first, at most limit (PRD section 13.3).
	ListByStatus(ctx context.Context, status proposal.Status, limit int) ([]*proposal.Proposal, error)
}

// SubjectResolver maps the subject strings of lessons to the subjects of the library, which key the agent threads
// (ADR-0024, PRD section 9). Implemented by cases/library, wired by the app.
type SubjectResolver interface {
	// ResolveSubjects returns the library subject of each name that matches one (PRD section 9).
	ResolveSubjects(ctx context.Context, names []string) (map[string]subjectmatch.Match, error)
}

// Clock supplies the current time in the student's zone, whose calendar days the screen uses (ARCH-24, PRD
// section 9). Implemented by the clock adapter.
type Clock interface {
	// Now returns the current time (PRD section 9).
	Now() time.Time
}
