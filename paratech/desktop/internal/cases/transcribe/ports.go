package transcribe

import (
	"context"
	"io"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/sttmodel"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// STTEngine runs local speech recognition (PRD sections 4.3 and 8, AC-06). Implemented by the whispercli adapter
// (TASK-024) which starts the whisper-cli sidecar as a separate CPU-only process for each call; a fake serves tests.
// There is no cloud implementation and there must never be one.
//
// Job progress: the transcribe handler turns the progress callback into job.Progress reports to the job runner
// (the report function it receives in Handle) and does not save the job itself, because the runner is the only
// owner of a claimed job (ADR-0004). It receives the job as a read-only job.Snapshot, not as the aggregate.
//
// The adapter reads and writes below the artifact root it was wired with; references keep the core free of OS paths.
type STTEngine interface {
	// Transcribe recognises the audio file audio with the model file model and writes the result to output.
	// progress, if not nil, is called with a percentage from 0 to 100 (PRD section 7.3: persistent progress).
	// Cancelling ctx kills the process and leaves no partial output behind (PRD section 4.3).
	Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, progress func(percent int)) error
}

// TranscriptRepository stores the transcript aggregate in the local database (PRD sections 5.1 and 8, AC-08).
// Implemented by the sqlite adapter.
type TranscriptRepository interface {
	// Get loads a transcript (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error)

	// Save inserts or updates a transcript with the pinned model name and version (PRD section 8, AC-08). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	Save(ctx context.Context, t *transcript.Transcript) error
}

// RecordingReader gives read access to the recording being transcribed (PRD sections 6.2 and 8).
// Implemented by the sqlite adapter.
type RecordingReader interface {
	// Get loads a recording (PRD section 6.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)
}

// ModelReader gives read access to installed STT models, for the model a job pins (PRD section 8, AC-08).
// Implemented by the sqlite adapter.
type ModelReader interface {
	// Get loads an installed model (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.SttModelID) (*sttmodel.Model, error)
}

// SettingsReader gives read access to the user settings, for the default STT model and the power switch
// (PRD sections 7.4 and 8, AC-07, AC-22). Implemented by the sqlite adapter.
type SettingsReader interface {
	// Load returns the current settings, defaults applied (PRD sections 7.4 and 15).
	Load(ctx context.Context) (*settings.Settings, error)
}

// JobEnqueuer creates the follow-up jobs of a finished transcription, for example summarisation
// (PRD sections 7.1 and 17 phase 6). Implemented by the sqlite adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID of the
	// existing job is returned with created == false (PRD section 7.3, hard rule 7).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// PowerMonitor reports the power source so that a running STT job can stop at the next checkpoint when AC power is
// lost (PRD section 7.4, section 14 case 12, AC-21). Implemented by a Windows adapter (TASK-011) and by a fake elsewhere.
type PowerMonitor interface {
	// IsOnACPower reports whether the computer runs on AC power (PRD section 7.4). Where it cannot be determined the
	// adapter reports true.
	IsOnACPower(ctx context.Context) (bool, error)
}

// ArtifactStore is the access of this use case to audio inputs and transcript files. All file access goes through
// the Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact; the caller closes it. A missing artifact yields shared.ErrNotFound
	// (PRD section 5.2).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)

	// Write stores the content atomically (a temporary file and a rename), so a crash never leaves a torn file
	// (PRD section 5.2).
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error

	// List returns the direct children of the directory dir sorted by name, for example per-chunk results
	// (PRD section 6.2). A missing directory yields shared.ErrNotFound.
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)
}

// Clock supplies the current time (ARCH-24; PRD section 8). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 8: model and transcript timestamps).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 8). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewTranscriptID returns a fresh transcript identifier (PRD section 8).
	NewTranscriptID() shared.TranscriptID

	// NewJobID returns a fresh job identifier for follow-up jobs (PRD section 7.1).
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
