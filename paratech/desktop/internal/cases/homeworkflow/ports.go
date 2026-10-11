package homeworkflow

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
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

	// Save inserts or updates an assignment (PRD section 10: review and edit locally). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	// A stale version happens for example when the user edited the assignment while a reprocessing job produced new
	// values.
	Save(ctx context.Context, a *assignment.Assignment) error
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
	// Now returns the current time (PRD section 10: draft versions and timestamps).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 10). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewAssignmentID returns a fresh assignment identifier (PRD section 10).
	NewAssignmentID() shared.AssignmentID

	// NewJobID returns a fresh job identifier (PRD section 7.1).
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
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
