package homeworkflow

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/settings/settings"
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
	// Get loads an assignment (PRD section 10), a deleted one too (ADR-0029: «Вернуть»; use cases hide it from
	// readers). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error)

	// GetBySource loads the assignment made from item index of the extraction answer of transcript t, so that
	// reprocessing updates it instead of adding a second one (PRD sections 9 and 7.3). A deleted one is loaded too: it
	// is the tombstone that reprocessing never revives (ADR-0029). It returns an error wrapping shared.ErrNotFound if
	// there is none.
	GetBySource(ctx context.Context, t shared.TranscriptID, index int) (*assignment.Assignment, error)

	// ListByTranscript loads the assignments extracted from transcript t in the order of the extraction answer, so
	// that reprocessing finds the ones a shorter answer no longer has (PRD section 10). Deleted ones are skipped
	// (ADR-0029; reprocessing never withdraws them anyway). None is an empty list.
	ListByTranscript(ctx context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error)

	// Busy reports whether something in progress uses assignment id, so that DeleteAssignment refuses it with
	// assignment.ErrInUse (PRD section 10, ADR-0029): a delivery in PENDING (a scheduled one too), SENDING or UNKNOWN, a work in triage
	// or writing, or an unfinished draft generation or writing job. Called inside Tx.Do it reads in that transaction.
	Busy(ctx context.Context, id shared.AssignmentID) (bool, error)

	// Delete removes an assignment reprocessing withdrew (PRD section 10, assignment.Superseded). It is a version
	// compare-and-swap (ADR-0004(d)): a row that changed or is gone yields shared.ErrVersionConflict. An assignment
	// that other records (a delivery) reference is kept and yields assignment.ErrInUse.
	Delete(ctx context.Context, a *assignment.Assignment) error

	// DeleteUnused removes an assignment the student added, for the Undo of its creation (PRD section 10, ADR-0029).
	// It is the version compare-and-swap of Delete (shared.ErrVersionConflict), and in the same transaction it refuses
	// with assignment.ErrInUse an assignment that a draft, an unfinished draft generation, a delivery, a turnkey work
	// or a proposal other than a declined or rejected one references; works are checked explicitly, their foreign key
	// would cascade.
	DeleteUnused(ctx context.Context, a *assignment.Assignment) error

	// Save inserts or updates an assignment (PRD section 10: review and edit locally). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// A stale version happens for example when the user edited the assignment while a reprocessing job produced new
	// values. A second assignment for the same extraction item (GetBySource) yields shared.ErrDuplicate.
	Save(ctx context.Context, a *assignment.Assignment) error
}

// AssignmentList lists the stored assignments for the homework screen (PRD section 10 step 1: select an assignment).
// Implemented by the sqlite adapter. It is a port of its own so that the fakes of AssignmentRepository stay small.
type AssignmentList interface {
	// ListRecent returns at most limit assignments, newest first (by creation time; PRD section 10), deleted ones
	// skipped (ADR-0029); none is an empty list. limit is at least 1.
	ListRecent(ctx context.Context, limit int) ([]*assignment.Assignment, error)
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

// LectureTextRepository stores the lecture texts of transcripts with the finished parts of their runs (PRD section 9,
// TASK-090). Implemented by the sqlite adapter.
type LectureTextRepository interface {
	// Get loads a lecture text (PRD section 9). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id lecturetext.ID) (*lecturetext.LectureText, error)

	// Save inserts or updates a lecture text (PRD section 9). It is the version compare-and-swap of ADR-0004(d), like
	// SummaryRepository.Save: an insert of an existing id or a stale version yields shared.ErrVersionConflict.
	Save(ctx context.Context, l *lecturetext.LectureText) error
}

// DraftRepository stores the draft history of each assignment (PRD section 10, AC-13). Implemented by the sqlite
// adapter (a later task); an in-memory fake serves tests.
type DraftRepository interface {
	// Get loads the draft of an assignment (PRD section 10). It returns an error wrapping shared.ErrNotFound if there
	// is none.
	Get(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error)

	// Save inserts or updates a draft with all its versions (PRD section 10: draft versions are kept). It is the
	// version compare-and-swap of ADR-0004(d), like AssignmentRepository.Save: Version() == 0 is INSERTed (an
	// existing draft of the assignment yields shared.ErrVersionConflict), a stale version yields
	// shared.ErrVersionConflict and nothing is written. Versions are append-only, so an adapter may write only the
	// ones it has not stored yet.
	Save(ctx context.Context, d *homeworkdraft.Draft) error
}

// RecordingReader gives read access to the recording a transcript was made from, for the lesson it belongs to and its
// start time (PRD sections 6.2 and 9.1). Implemented by the sqlite adapter.
type RecordingReader interface {
	// Get loads a recording (PRD section 6.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// TranscriptRecordings finds the recording an assignment's transcript was made from, for the lesson it kept at the start
// (TASK-088): subject, kind, teacher and date of the assignment (PRD sections 10 and 11, TASK-092). Implemented by the
// sqlite adapter.
type TranscriptRecordings interface {
	// RecordingOf loads the recording transcript t was made from (PRD section 6.2); a missing transcript or recording
	// wraps shared.ErrNotFound.
	RecordingOf(ctx context.Context, t shared.TranscriptID) (*recording.Recording, error)
}

// LessonReader gives read access to the schedule entry of a recording: its title and start are the minimal lesson
// context of an LLM operation (PRD sections 9.1 and 11). Implemented by the sqlite adapter.
type LessonReader interface {
	// Get loads a live lesson (PRD section 11). It returns an error wrapping shared.ErrNotFound if there is none or it
	// was removed from the schedule.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)

	// ListStartingBetween returns the live lessons whose start lies in [from, to), ordered by start (PRD section 11);
	// the post-processing looks up the next lesson of the subject in it (TASK-093). Soft-deleted lessons are not listed.
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)
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

// DraftReader reads the draft history of an assignment (PRD section 10): the agent's send names a draft version
// (TASK-127), and the confirmation shows which version the pinned attachment is. Implemented by the sqlite adapter.
type DraftReader interface {
	// Get loads the draft of an assignment (PRD section 10); none wraps shared.ErrNotFound.
	Get(ctx context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error)
}

// WaitingProposals lists the proposals in one status (PRD section 13.3), so the same deadline or send of a draft
// version proposed again reuses the one that waits for «Да» (TASK-127). Implemented by the sqlite adapter.
type WaitingProposals interface {
	// ListByStatus returns the proposals in status, newest first, at most limit (PRD section 13.3).
	ListByStatus(ctx context.Context, status proposal.Status, limit int) ([]*proposal.Proposal, error)
}

// WorkFiles reads a turnkey work and the library around its finished file, for the send of the work to the teacher
// (PRD section 10 steps 7-10, TASK-180). Implemented by the app over the work repository and cases/library.
type WorkFiles interface {
	// Work loads a work (PRD section 10, ADR-0025); none wraps shared.ErrNotFound.
	Work(ctx context.Context, id shared.WorkID) (*work.Work, error)

	// Material returns a library material in any status (PRD section 10, ADR-0024); none wraps shared.ErrNotFound.
	Material(ctx context.Context, id shared.MaterialID) (*material.Material, error)

	// Subjects lists the subjects of the library with their folders (PRD section 10, ADR-0024).
	Subjects(ctx context.Context) ([]*subject.Subject, error)
}

// SubjectNames reads the current name of a library subject, for an assignment the student adds with it
// (PRD section 10, ADR-0029). Implemented by the app over cases/library; display only, nothing is written.
type SubjectNames interface {
	// SubjectName returns the name of subject id (PRD section 10, ADR-0024); an unknown subject wraps
	// shared.ErrNotFound.
	SubjectName(ctx context.Context, id shared.SubjectID) (string, error)
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

// DeadlineRemovals is the queue of the Google deadline events of deleted assignments (PRD section 11.3; ADR-0029, the
// deadline_event_removals pattern of ADR-0018): the calendar sync deletes the event (gone is success) and then the row.
// Google is only a view of local data (hard rule 6). Implemented by the sqlite adapter; both calls join the caller's
// transaction.
type DeadlineRemovals interface {
	// QueueDeadlineRemoval stores the removal of the event of assignment id, queued at at; one already queued stays as
	// it is (PRD section 11.3; ADR-0029: «Удалить»).
	QueueDeadlineRemoval(ctx context.Context, id shared.AssignmentID, at time.Time) error

	// PurgeDeadlineRemoval forgets a removal still waiting; none is not an error (PRD section 11.3; ADR-0029:
	// «Вернуть»).
	PurgeDeadlineRemoval(ctx context.Context, id shared.AssignmentID) error
}

// The ports of the Purger (ADR-0029 addendum 2): an assignment deleted longer than assignment.RestoreWindow is removed
// in one transaction, the assignment row last. Implemented by the sqlite repositories and the filestore; each call is
// one statement, joining the caller's transaction. Every delete repeats the guard "still deleted before cutoff", so a
// restore that won keeps everything.

// DeletedAssignments finds the deleted assignments whose restore window ended and removes their rows (PRD section 10).
type DeletedAssignments interface {
	// ListDeletedBefore returns at most limit assignments deleted before cutoff, oldest delete first, after the keyset
	// (afterAt, afterID) of the last one of the previous page (zero afterAt: from the start); none is an empty list
	// (PRD section 10).
	ListDeletedBefore(ctx context.Context, cutoff, afterAt time.Time, afterID shared.AssignmentID,
		limit int) ([]*assignment.Assignment, error)

	// StillDeletedBefore reports whether assignment id is still deleted before cutoff; false when it is gone, restored
	// or deleted later (PRD section 10).
	StillDeletedBefore(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error)

	// PurgeDeleted removes the row of assignment id only if it is still deleted before cutoff, so a restore racing the
	// purge keeps it; false when no such row is left. A draft or a delivery still referencing it keeps the row and
	// wraps assignment.ErrInUse (PRD section 10).
	PurgeDeleted(ctx context.Context, id shared.AssignmentID, cutoff time.Time) (bool, error)
}

// PurgedDeliveries reads and removes the email deliveries of a deleted assignment (PRD section 10, AC-15).
type PurgedDeliveries interface {
	// ListByAssignment returns at most limit deliveries of assignment a, newest first (PRD section 10).
	ListByAssignment(ctx context.Context, a shared.AssignmentID, limit int) ([]*delivery.Delivery, error)

	// DeleteFinalOfAssignment removes the SENT and FAILED deliveries of assignment a, only while a is still deleted
	// before cutoff, and returns how many; an unresolved one is never removed (PRD section 10, AC-15, hard rule 7).
	DeleteFinalOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error)
}

// PurgedProposals removes the action proposals naming a deleted assignment (PRD section 13.3).
type PurgedProposals interface {
	// DeleteOfAssignment removes every proposal of assignment a, only while a is still deleted before cutoff, and
	// returns how many (PRD section 13.3).
	DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error)
}

// PurgedDrafts removes the draft of a deleted assignment with its versions (PRD section 10).
type PurgedDrafts interface {
	// DeleteOfAssignment removes the draft of assignment a, only while a is still deleted before cutoff, and returns
	// how many drafts went; none is not an error (PRD section 10).
	DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error)
}

// PurgedWorks removes the turnkey works of a deleted assignment with their rounds (PRD section 10, ADR-0025).
type PurgedWorks interface {
	// DeleteOfAssignment removes the works of assignment a, only while a is still deleted before cutoff, and returns
	// how many (PRD section 10).
	DeleteOfAssignment(ctx context.Context, a shared.AssignmentID, cutoff time.Time) (int, error)
}

// FileRemover deletes files through the Storage Manager (PRD section 5.2, hard rule 5). Implemented by the filestore
// adapter.
type FileRemover interface {
	// Delete removes a file or a directory tree; what is already gone is not an error (PRD section 5.2).
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// CalendarSyncRequester asks for a calendar sync, which removes the queued deadline events (PRD section 11.3).
// Implemented in the app over the calendar sync; it only logs, and without a sign-in it does nothing: the queue waits
// for the next sync.
type CalendarSyncRequester interface {
	// RequestSync asks for a sync run (PRD section 11.3).
	RequestSync(ctx context.Context)
}

// UnresolvedDeliveries tells whether an email of an assignment may still go out or may have gone out unseen, so that
// Drafts.ExportDraft does not overwrite the file that email attaches (PRD section 10, AC-15, hard rule 7).
// Implemented by senddelivery.Service.
type UnresolvedDeliveries interface {
	// HasUnresolved reports whether assignment a has a delivery in PENDING, SENDING or UNKNOWN (PRD section 10). An
	// error means the deliveries could not be read.
	HasUnresolved(ctx context.Context, a shared.AssignmentID) (bool, error)
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

// JobReader gives read access to a job of this workflow, so that a Generate whose idempotency key belongs to a job
// that failed or was cancelled can start the generation again (PRD sections 7.2, 10 and 14 case 5), and so that the
// screen and Generate see the generation an assignment has going (TASK-094). Implemented by the sqlite adapter.
type JobReader interface {
	// Get loads a job (PRD section 7.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.JobID) (*job.Job, error)

	// LatestByKeyPrefix returns the newest job of type typ, in any status, whose idempotency key starts with prefix
	// (PRD section 7.2); none wraps shared.ErrNotFound. Called inside Tx.Do it reads in that transaction.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)
}

// ProfileSource reads the student profile from the local settings, for the title page of a Word file (PRD section 10
// step 6, TASK-094). Implemented by the sqlite settings repository.
type ProfileSource interface {
	// Load returns the stored settings, or the defaults (an empty profile) while nothing is stored (PRD section 10).
	Load(ctx context.Context) (*settings.Settings, error)
}

// DocumentRenderer renders a homework document as a Word file (PRD section 10 step 6, TASK-094). Implemented by the
// docx adapter.
type DocumentRenderer interface {
	// Render returns the .docx bytes of doc; the same document gives the same bytes (PRD section 10: the attachment is
	// pinned by its digest).
	Render(ctx context.Context, doc values.Document) ([]byte, error)
}

// DocumentWriter writes a file to the path the student chose in the save dialog, atomically (a failure leaves no half
// file under that name; PRD sections 5.2 and 10 step 6). Implemented by the textfile adapter; errors never carry the
// path.
type DocumentWriter interface {
	// WriteFile replaces the file at path with content (PRD section 10 step 6).
	WriteFile(ctx context.Context, path string, content []byte) error
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

// LatestTranscripts finds the newest transcript of a recording, whose post-processing the user starts again
// (PRD section 9, TASK-086). Implemented by the sqlite adapter.
type LatestTranscripts interface {
	// LatestByRecording loads the newest transcript of the recording (PRD section 8); none wraps shared.ErrNotFound.
	LatestByRecording(ctx context.Context, rec shared.RecordingID) (*transcript.Transcript, error)
}

// MaterialTexts reads a library material and its text for its summary and homework search (PRD sections 9 and 10,
// TASK-122). Implemented by cases/library (library.Service, wired by the app).
type MaterialTexts interface {
	// MaterialText returns the material and its text (PRD section 9). A missing material wraps shared.ErrNotFound, a
	// removed one material.ErrRemoved; a text that is not read material.ErrTextNotExtracted, an unreadable one
	// material.ErrTextUnreadable, damaged bytes material.ErrDamaged.
	MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error)
}

// MaterialLookup finds a library material before its work starts (PRD section 9, TASK-122). Implemented by
// cases/library, wired by the app.
type MaterialLookup interface {
	// Material returns the material in any status (PRD section 9); a missing one is an error of the library.
	Material(ctx context.Context, id shared.MaterialID) (*material.Material, error)
}

// MaterialAssignments finds the assignments extracted from a library material, like GetBySource and
// ListByTranscript for a transcript (PRD section 10, TASK-122). Implemented by the sqlite adapter. A port of its own
// so that the fakes of AssignmentRepository stay small.
type MaterialAssignments interface {
	// GetByMaterialSource loads the assignment of item index of the extraction of material m (PRD section 10), a
	// deleted one too (the tombstone, ADR-0029); none wraps shared.ErrNotFound.
	GetByMaterialSource(ctx context.Context, m shared.MaterialID, index int) (*assignment.Assignment, error)

	// ListByMaterial loads the assignments extracted from material m in the order of the answer (PRD section 10),
	// deleted ones skipped; none is an empty list.
	ListByMaterial(ctx context.Context, m shared.MaterialID) ([]*assignment.Assignment, error)
}

// PostJobs finds the newest post-processing job of a transcript and creates the one that follows it (PRD section 9,
// TASK-086). Implemented by the sqlite adapter.
type PostJobs interface {
	// LatestByKeyPrefix returns the newest job of type typ, in any status, whose idempotency key starts with prefix
	// (PRD section 7.2); none wraps shared.ErrNotFound.
	LatestByKeyPrefix(ctx context.Context, typ job.Type, prefix string) (*job.Job, error)

	// Enqueue stores a new job (PRD section 7.1); for an existing idempotency key it creates nothing and returns the
	// existing job's ID with created == false.
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// SummaryReader reads the summary of a transcript (PRD section 9). Implemented by the sqlite adapter.
type SummaryReader interface {
	// Get loads a summary (PRD section 9); none wraps shared.ErrNotFound.
	Get(ctx context.Context, id summary.ID) (*summary.Summary, error)
}
