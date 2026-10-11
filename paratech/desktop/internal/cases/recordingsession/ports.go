package recordingsession

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// RecordingRepository stores the recording aggregate in the local database, the system of record
// (PRD sections 5.1 and 6.2). Implemented by the sqlite adapter.
type RecordingRepository interface {
	// Get loads a recording (PRD section 6.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)

	// Save inserts or updates the full state of a recording, including status and heartbeat
	// (PRD section 6.2: persistent state, watchdog).
	Save(ctx context.Context, r *recording.Recording) error

	// ListByStatus returns the recordings in any of the given statuses. Startup recovery uses it to find
	// sessions that were still active when the app died (PRD section 6.2, section 14 case 1, AC-05).
	ListByStatus(ctx context.Context, statuses ...recording.Status) ([]*recording.Recording, error)

	// Delete removes the recording metadata; its transcripts go with it (PRD section 13.1, clear deletion
	// semantics). The audio files are removed separately through ArtifactStore.
	Delete(ctx context.Context, id shared.RecordingID) error
}

// Recorder captures system audio (WASAPI loopback) into chunk files, one track per recording
// (PRD sections 4.3, 6.2 and 6.3, AC-24). Implemented by a Windows adapter (TASK-017) and by a fake
// elsewhere. Audio never leaves the machine (AC-06).
//
// The adapter writes below the artifact root it was wired with; references keep the core free of OS paths.
type Recorder interface {
	// Start begins capturing for the recording id and writes chunk files into the directory chunks
	// (PRD section 6.3: chunked writes, a crash loses at most the latest chunk). It returns once capture runs.
	Start(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef) error

	// Stop ends the capture of id and flushes the last chunk (PRD section 6.2: STOPPING).
	Stop(ctx context.Context, id shared.RecordingID) error

	// Wait blocks until the capture of id has ended. It returns nil after a Stop and an error if the capture
	// ended by itself, for example the output device changed or failed, so that the use case can move the
	// recording to FAILED or RECOVERY_REQUIRED (PRD section 6.2). Cancelling ctx stops waiting, not capturing.
	Wait(ctx context.Context, id shared.RecordingID) error

	// Finalize assembles the readable chunks in the directory chunks into the single file output and returns
	// its duration (PRD section 6.2: FINALIZING, recovery after a restart). It is idempotent and does not
	// need a running capture.
	Finalize(ctx context.Context, chunks, output shared.ArtifactRef) (time.Duration, error)
}

// ArtifactStore is the access of this use case to recording files. All file access goes through
// the Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// List returns the direct children of the directory dir sorted by name, for example the chunk files
	// of an interrupted recording (PRD section 6.2). A missing directory yields shared.ErrNotFound.
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)

	// Delete removes a file or a whole directory tree. Deleting what is already gone is not an error
	// (PRD section 13.1: deletion semantics, idempotent).
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// SettingsReader gives read access to the user settings, for example whether automatic recording prompts
// are enabled (PRD sections 6.1 and 15, AC-02). Implemented by the sqlite adapter.
type SettingsReader interface {
	// Load returns the current settings, defaults applied (PRD section 15).
	Load(ctx context.Context) (*settings.Settings, error)
}

// JobEnqueuer creates background jobs, for example the transcription job of a completed recording
// (PRD sections 7.1 and 7.3). It is a separate step after the recording was saved, repeatable thanks to
// the job idempotency key (ARCH-23). Implemented by the sqlite adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and
	// the ID of the existing job is returned with created == false (PRD section 7.3, section 14 case 5).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)
}

// Clock supplies the current time (ARCH-24), for the heartbeat and the duration cap
// (PRD section 6.2). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 6.2).
	Now() time.Time
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 6.2). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewRecordingID returns a fresh recording identifier (PRD section 6.2).
	NewRecordingID() shared.RecordingID
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The transaction boundary is one use case method call and it changes
// one aggregate. Implemented by the sqlite adapter (TASK-005).
type Tx interface {
	// Do runs fn in a database transaction and commits if it returns nil, otherwise it rolls back and returns
	// the error (PRD sections 5.1 and 7.3: persistent state transitions). Repositories called with the context
	// passed to fn take part in the transaction; calls with another context do not.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
