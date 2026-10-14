package schedulesync

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
)

// LessonRepository stores the local schedule, the system of record (PRD sections 5.3 and 11.1, AC-20).
// Implemented by the sqlite adapter.
//
// Lessons are deleted softly (ADR-0004): removing a lesson from the schedule marks it deleted (the aggregate sets
// deleted_at) and the row stays until the Google Calendar event is gone, so the calendar synchronization can still
// delete the event of a lesson the user no longer has (PRD section 11.3, AC-16). Deleted lessons are invisible to
// Get and ListStartingBetween and visible to ListPendingCalendarSync, and PurgeDeleted ends their life.
type LessonRepository interface {
	// Get loads a lesson (PRD section 11). It returns an error wrapping shared.ErrNotFound if there is none or the
	// lesson is soft-deleted.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)

	// Save inserts or updates a lesson (PRD section 11). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// A stale version happens for example when an import changed the lesson while the calendar synchronization marked
	// it synchronized: the synchronization marks the lesson synced and saves the same instance once, and reloads on
	// shared.ErrVersionConflict.
	Save(ctx context.Context, l *lesson.Lesson) error

	// ApplyChanges stores the upserted lessons and the lessons the import deleted in ONE transaction. It is the
	// commit of an import: either the whole previewed change applies or nothing does (PRD sections 11.2 and 14
	// case 9, AC-16). This batch of same-type aggregates is the documented exception to the one-aggregate rule
	// of ARCH-23 (ADR-0004); the calendar synchronization job is enqueued afterwards as a separate repeatable step.
	// Every lesson is saved with the version compare-and-swap of Save (the adapter calls Persisted on each one after the
	// commit) and any shared.ErrVersionConflict rolls back the whole batch.
	// The deleted lessons were already marked deleted through the aggregate; the adapter persists them as soft
	// deleted rows and never issues a DELETE.
	ApplyChanges(ctx context.Context, upserts, deleted []*lesson.Lesson) error

	// ListLive returns every live (not soft-deleted) lesson, ordered by start. The import diffs a file against it and
	// finds the conflicts with lessons it keeps (PRD sections 11.2 and 14 case 9).
	ListLive(ctx context.Context) ([]*lesson.Lesson, error)

	// ListStartingBetween returns the live lessons whose start lies in [from, to), ordered by start. The scheduler
	// uses it to find lessons that are about to begin (PRD section 6.1). Soft-deleted lessons are not returned.
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)

	// ListPendingCalendarSync returns up to limit lessons whose calendar projection is out of date
	// (PRD section 11.3: controlled synchronization). The predicate is a domain rule, not an adapter decision; it is
	// stated here in terms of explicit lesson fields and the lesson aggregate exposes the same rule as a method
	// (TASK-018): a lesson is pending when it is soft-deleted (until PurgeDeleted removes it), or it was never written to the
	// calendar (calendar_synced_revision is NULL), or its content_revision is greater than the revision last written
	// (calendar_synced_revision). Revision counters are used instead of comparing timestamps, so two edits in the
	// same millisecond or a clock change cannot hide a change. Unlike Get, this list includes deleted lessons, so
	// that CalendarAdapter.DeleteEvent can act on them.
	ListPendingCalendarSync(ctx context.Context, limit int) ([]*lesson.Lesson, error)

	// PurgeDeleted removes the row of a soft-deleted lesson after the deletion of its calendar event was confirmed
	// (PRD section 11.3). It is idempotent: a missing row is not an error. Purging a lesson that is not deleted
	// returns an error wrapping shared.ErrConflict, so a live lesson can never be removed by mistake. References from
	// recordings and assignments become NULL (ON DELETE SET NULL in the schema).
	PurgeDeleted(ctx context.Context, id shared.LessonID) error
}

// ScheduleParser reads one import format (CSV, ICS or XLSX) into rows (PRD section 11.2, AC-16). Implemented by one
// adapter per format (csvschedule for CSV, icsschedule for ICS). It only reads: nothing is stored, and the same bytes
// give the same rows (for ICS within one day: its import dates follow the clock), so the commit can parse the file
// again and compare with the preview.
type ScheduleParser interface {
	// Parse reads the whole file (PRD section 11.2). Problems of single rows are returned as data in
	// importplan.Parsed.Errors with their line numbers; an error wrapping importplan.ErrUnreadableFile means the file
	// cannot be read at all (encoding, header, size).
	Parse(ctx context.Context, data []byte) (importplan.Parsed, error)
}

// CalendarAdapter isolates the Google Calendar API from the core (PRD sections 4.1 and 11.3, AC-17). Implemented by
// the googlecal adapter (TASK-051) and by a fake in tests. It only ever touches the secondary calendar the app
// created itself (scope calendar.app.created). Local data stays the source of truth: nothing is read back (AC-20).
// Authorization tokens live in the credential store inside the adapter, never in the core (PRD section 12).
//
// Event identity: the adapter derives the event ID deterministically from the lesson ID (a stable encoding that
// satisfies Google's event ID rules). Nothing is stored in SQLite for it, so the core never keeps an external ID.
type CalendarAdapter interface {
	// EnsureCalendar returns the ID of the dedicated "Learning Assistant" calendar and creates it if it does not
	// exist. It is idempotent (PRD section 11.3).
	EnsureCalendar(ctx context.Context) (calendarID string, err error)

	// UpsertLesson creates or updates the event of a lesson in the calendar calendarID. Because the event ID is
	// derived from the lesson ID, repeating the call updates the same event and never creates a duplicate
	// (PRD section 11.3 and section 14 case 8: stable external IDs, idempotent upserts, hard rule 7).
	UpsertLesson(ctx context.Context, calendarID string, l *lesson.Lesson) error

	// DeleteEvent removes the event of the lesson id from the calendar calendarID, using the same derived event ID.
	// Deleting an event that is already gone, or one that was never created, is not an error (PRD section 11.3:
	// idempotent).
	DeleteEvent(ctx context.Context, calendarID string, id shared.LessonID) error
}

// JobEnqueuer creates the calendar synchronization job after a schedule commit (PRD sections 7.1 and 11.3). It is a
// separate, repeatable step after the commit (ARCH-23, ADR-0004): a crash between the two leaves pending lessons
// that the next enqueue picks up. Implemented by the sqlite adapter.
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

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary
// is one use case method call and it changes one aggregate. The two documented exceptions (ADR-0004) are the batch
// import of LessonRepository.ApplyChanges and the deletion of a recording together with its transcripts
// (RecordingTranscripts.DeleteByRecording and RecordingRepository.Delete in one Tx.Do). Implemented by the sqlite
// adapter (TASK-005).
type Tx interface {
	// Do runs fn in one write transaction and commits if fn returns nil, otherwise it rolls back and returns the
	// error (PRD section 7.3: persistent state transitions). The sqlite adapter opens a write transaction up front
	// (BEGIN IMMEDIATE), so a read inside fn cannot be invalidated by another writer before the commit.
	// Repositories called with the context passed to fn take part in the transaction; calls with another context
	// do not. Nesting: a Do called with the context of a running fn joins the outer transaction (no savepoint) and
	// only the outermost Do commits or rolls back. fn performs no external I/O (network, e-mail, calendar, files,
	// subprocesses): it holds the database write lock and only reads and writes local aggregates.
	// When Do returns an error, aggregates saved inside it must be reloaded before reuse: Save records the new
	// version on the instance (Persisted) before the outermost commit, which a rollback does not undo (ADR-0004(d)).
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
