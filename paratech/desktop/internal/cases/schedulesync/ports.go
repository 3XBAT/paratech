package schedulesync

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// LessonRepository stores the local schedule, the system of record (PRD sections 5.3 and 11.1, AC-20).
// Implemented by the sqlite adapter.
type LessonRepository interface {
	// Get loads a lesson (PRD section 11). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)

	// Save inserts or updates a lesson (PRD section 11).
	Save(ctx context.Context, l *lesson.Lesson) error

	// ApplyChanges stores the upserted lessons and removes the deleted ones in ONE transaction. It is the commit
	// of an import: either the whole previewed change applies or nothing does (PRD sections 11.2 and 14 case 9,
	// AC-16). This batch is a deliberate reading of ARCH-23 for imports; see the TASK-003 report.
	ApplyChanges(ctx context.Context, upserts []*lesson.Lesson, deleted []shared.LessonID) error

	// ListStartingBetween returns the lessons whose start lies in [from, to), ordered by start. The scheduler
	// uses it to find lessons that are about to begin (PRD section 6.1).
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)

	// ListPendingCalendarSync returns up to limit lessons that are new or changed since they were last written to
	// the calendar (PRD section 11.3: controlled synchronization).
	ListPendingCalendarSync(ctx context.Context, limit int) ([]*lesson.Lesson, error)
}

// CalendarAdapter isolates the Google Calendar API from the core (PRD sections 4.1 and 11.3, AC-17). Implemented by
// the googlecal adapter (TASK-051) and by a fake in tests. It only ever touches the secondary calendar the app
// created itself (scope calendar.app.created). Local data stays the source of truth: nothing is read back (AC-20).
// Authorization tokens live in the credential store inside the adapter, never in the core (PRD section 12).
type CalendarAdapter interface {
	// EnsureCalendar returns the ID of the dedicated "Learning Assistant" calendar and creates it if it does not
	// exist. It is idempotent (PRD section 11.3).
	EnsureCalendar(ctx context.Context) (calendarID string, err error)

	// UpsertLesson creates or updates the event of a lesson in the calendar calendarID and returns the event ID.
	// The event ID is derived from the lesson ID, so repeating the call never creates a duplicate
	// (PRD section 11.3 and section 14 case 8: stable external IDs, idempotent upserts, hard rule 7).
	UpsertLesson(ctx context.Context, calendarID string, l *lesson.Lesson) (eventID string, err error)

	// DeleteEvent removes an event from the calendar calendarID. Deleting an event that is already gone is
	// not an error (PRD section 11.3: idempotent).
	DeleteEvent(ctx context.Context, calendarID, eventID string) error
}

// JobEnqueuer creates the calendar synchronization job after a schedule commit (PRD sections 7.1 and 11.3). It is a
// separate, repeatable step (ARCH-23). Implemented by the sqlite adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID of the
	// existing job is returned with created == false (PRD section 7.3, hard rule 7).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// Clock supplies the current time (ARCH-24), for the scheduler (PRD section 6.1). Implemented by the clock adapter
// and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 6.1: lesson is starting).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 11). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewLessonID returns a fresh lesson identifier for an imported or manual entry (PRD section 11).
	NewLessonID() shared.LessonID

	// NewJobID returns a fresh job identifier for the synchronization job (PRD section 7.1).
	NewJobID() shared.JobID
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary is one use case method call and it changes
// one aggregate. Implemented by the sqlite adapter (TASK-005).
type Tx interface {
	// Do runs fn in a database transaction and commits if it returns nil, otherwise it rolls back and returns
	// the error (PRD section 7.3: persistent state transitions). Repositories called with the context passed to
	// fn take part in the transaction; calls with another context do not.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
