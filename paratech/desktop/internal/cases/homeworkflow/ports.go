package homeworkflow

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// LLMClient sends one operation to the cloud LLM Hub and returns the answer (PRD sections 4.2, 4.4 and 9). Implemented
// by the hubclient adapter (TASK-038), which authenticates with the per-tester API key from the credential store; a
// fake serves tests. Only text and ids cross it: raw audio never does (AC-06).
//
// The result is UNTRUSTED model output (hard rule 3, PRD section 13.3). It is typed as values.UntrustedOutput, not a
// string, so that it cannot flow into another port by accident. Callers validate it against a strict schema and
// trusted local state; it must never reach an email, file or calendar adapter directly (AC-18). The request is a
// validated value object (values.NewLLMRequest), so the adapter never sees a URL as a model alias.
type LLMClient interface {
	// Complete runs the operation described by req. The Hub reserves quota before and settles after the call, and the
	// IdempotencyKey of req guarantees that a retry is not charged twice (PRD section 9.2, AC-11).
	// Errors the caller must map to job statuses: shared.ErrNetworkUnavailable (job waits in WAITING_FOR_NETWORK),
	// shared.ErrQuotaExceeded (WAITING_FOR_QUOTA); other failures are transient or permanent (PRD sections 7.2 and 9.2).
	Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error)
}

// AssignmentRepository stores assignments in the local database, the system of record (PRD sections 5.3 and 10,
// AC-20). Implemented by the sqlite adapter.
type AssignmentRepository interface {
	// Get loads an assignment (PRD section 10). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error)

	// GetBySource loads the assignment made from item index of the extraction answer of transcript t, so that
	// reprocessing updates it instead of adding a second one (PRD sections 9 and 7.3). It returns an error wrapping
	// shared.ErrNotFound if there is none.
	GetBySource(ctx context.Context, t shared.TranscriptID, index int) (*assignment.Assignment, error)

	// ListByTranscript loads the assignments extracted from transcript t in the order of the extraction answer, so
	// that reprocessing finds the ones a shorter answer no longer has (PRD section 10). None is an empty list.
	ListByTranscript(ctx context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error)

	// Delete removes an assignment reprocessing withdrew (PRD section 10, assignment.Superseded). It is a version
	// compare-and-swap (ADR-0004(d)): a row that changed or is gone yields shared.ErrVersionConflict. An assignment
	// that other records (a delivery) reference is kept and yields assignment.ErrInUse.
	Delete(ctx context.Context, a *assignment.Assignment) error

	// Save inserts or updates an assignment (PRD section 10: review and edit locally). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// A stale version happens for example when the user edited the assignment while a reprocessing job produced new
	// values. A second assignment for the same extraction item (GetBySource) yields shared.ErrDuplicate.
	Save(ctx context.Context, a *assignment.Assignment) error
}

// SummaryRepository stores the summaries of transcripts in the local database (PRD sections 5.3 and 9).
// Implemented by the sqlite adapter.
type SummaryRepository interface {
	// Get loads a summary (PRD section 9). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)

	// Save inserts or updates a summary (PRD section 9). It is the version compare-and-swap of ADR-0004(d), like
	// AssignmentRepository.Save: an insert of an existing id or a stale version yields shared.ErrVersionConflict.
	Save(ctx context.Context, s *summary.Summary) error
}

// DraftRepository stores the draft history of each assignment (PRD section 10, AC-13). Implemented by the sqlite
// adapter (a later task); an in-memory fake serves tests.
type DraftRepository interface {
	// Get loads the draft of an assignment (PRD section 10). It returns an error wrapping shared.ErrNotFound if there
	// is none.
	Get(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error)

	// Save inserts or updates a draft with all its versions (PRD section 10: draft versions are kept). It is the version compare-and-swap of ADR-0004(d), like
	// AssignmentRepository.Save: Version() == 0 is INSERTed (an existing draft of the assignment yields
	// shared.ErrVersionConflict), a stale version yields shared.ErrVersionConflict and nothing is written. Versions
	// are append-only, so an adapter may write only the ones it has not stored yet.
	Save(ctx context.Context, d *homeworkdraft.Draft) error
}

// RecordingReader gives read access to the recording a transcript was made from, for the lesson it belongs to and its
// start time (PRD sections 6.2 and 9.1). Implemented by the sqlite adapter.
type RecordingReader interface {
	// Get loads a recording (PRD section 6.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// LessonReader gives read access to the schedule entry of a recording: its title and start are the minimal lesson
// context of an LLM operation (PRD sections 9.1 and 11). Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson (PRD section 11). It returns an error wrapping shared.ErrNotFound if there is none or it
	// was removed from the schedule.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// ProposalRepository stores the actions the LLM proposed, with their validation and confirmation state
// (PRD section 13.3, AC-14, AC-18). Implemented by the sqlite adapter (a later task); an in-memory fake serves tests.
type ProposalRepository interface {
	// Get loads a proposal (PRD section 13.3). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id proposal.ID) (*proposal.Proposal, error)

	// Save inserts or updates a proposal (PRD section 13.3). It is the version compare-and-swap of ADR-0004(d), like
	// AssignmentRepository.Save: a stale version yields shared.ErrVersionConflict and nothing is written.
	Save(ctx context.Context, p *proposal.Proposal) error
}

// RecipientSource reads the configured homework recipient from the local settings, the trusted source of the address a
// homework is emailed to (PRD sections 10 and 13.3). The model never supplies it. Implemented over the settings by a
// later task (TASK-044).
type RecipientSource interface {
	// HomeworkRecipient returns the configured recipient, or the zero proposal.Recipient when none is configured
	// (PRD section 10). An error means the settings could not be read.
	HomeworkRecipient(ctx context.Context) (proposal.Recipient, error)
}

// The three side-effect ports below are the only way an action proposal reaches the outside world (PRD section 13.3,
// AC-18, ARCH-27). Each takes a command type that only a CONFIRMED proposal can produce, never model text, so model
// output cannot reach them directly. Each command carries an idempotency key: the use case may repeat a call after a
// crash or a failed save, and two concurrent executions may both call, so the implementation must make a repeat with
// the same key harmless (hard rule 7). Implementations reject a zero command.

// HomeworkMailer emails the exported homework of an assignment to the trusted recipient (PRD section 10 steps 7-10,
// AC-14). Implemented by a later task (TASK-044) through the delivery flow, which keys the delivery by the command's
// idempotency key and reconciles an unknown outcome instead of sending again (AC-15).
type HomeworkMailer interface {
	// SendHomework sends cmd.Attachment to cmd.Recipient (PRD section 10). A repeat with the same idempotency key
	// must not send a second email. The attachment must be exactly the content the user confirmed: the adapter reads
	// the file once, checks its SHA-256 and size against cmd.AttachmentDigest and sends those same bytes; on a
	// mismatch it sends nothing and returns an error wrapping proposal.ErrStale (the file changed after Execute's check).
	SendHomework(ctx context.Context, cmd proposal.EmailCommand) error
}

// HomeworkExporter writes homework text to the export file the core derived for the assignment (PRD sections 5.2 and
// 10 step 6). Implemented over the Storage Manager (hard rule 5); the target never comes from the model.
type HomeworkExporter interface {
	// ExportHomework writes cmd.Content to cmd.Target atomically (PRD section 10 step 6). Writing the same command
	// again yields the same file.
	ExportHomework(ctx context.Context, cmd proposal.ExportCommand) error
}

// DeadlineCalendar puts an assignment deadline into the app-created calendar (PRD section 11.3, AC-17, AC-20).
// Implemented by the googlecal adapter (TASK-051); the calendar is the adapter's dedicated one, never one the model names.
type DeadlineCalendar interface {
	// UpsertDeadline creates or updates the event of cmd.AssignmentID with a stable event id (PRD section 11.3:
	// idempotent upserts).
	UpsertDeadline(ctx context.Context, cmd proposal.CalendarCommand) error
}

// TranscriptReader gives read access to the transcript that a post-processing job works on (PRD sections 8 and 9.1).
// Implemented by the sqlite adapter.
type TranscriptReader interface {
	// Get loads a transcript (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error)
}

// ArtifactStore is the access of this use case to transcript text and exported homework files. All file access goes
// through the Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact, for example the transcript text; the caller closes it. A missing
	// artifact yields shared.ErrNotFound (PRD section 9.1).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// Write stores the content atomically (a temporary file and a rename), for example the exported final homework
	// artifact (PRD sections 5.2 and 10 step 6).
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
}

// JobEnqueuer creates the jobs of this workflow, for example summarisation and draft generation
// (PRD sections 7.1 and 14 case 5). Implemented by the sqlite adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID of the
	// existing job is returned with created == false, which guards against a duplicate Generate click
	// (PRD section 7.3, section 14 case 5).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// Clock supplies the current time (ARCH-24; PRD section 10). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time in the student's time zone (PRD section 10: draft versions and timestamps). Its
	// location is the zone in which lesson times are shown to the model and deadlines are read (PRD section 9.1).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 10). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewAssignmentID returns a fresh assignment identifier (PRD section 10).
	NewAssignmentID() shared.AssignmentID

	// NewJobID returns a fresh job identifier (PRD section 7.1).
	NewJobID() shared.JobID

	// NewProposalID returns a fresh identifier of an action proposal (PRD section 13.3).
	NewProposalID() proposal.ID
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
