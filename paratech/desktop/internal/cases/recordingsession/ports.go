package recordingsession

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// RecordingRepository stores the recording aggregate in the local database, the system of record
// (PRD sections 5.1 and 6.2). Implemented by the sqlite adapter.
type RecordingRepository interface {
	// Get loads a recording (PRD section 6.2). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)

	// Save inserts or updates the full state of a recording, including status and heartbeat
	// (PRD section 6.2: persistent state, watchdog). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). On success the adapter calls
	// Persisted(newVersion) on the aggregate, so saving the same instance again works.
	Save(ctx context.Context, r *recording.Recording) error

	// ListByStatus returns the recordings in any of the given statuses. Startup recovery uses it to find
	// sessions that were still active when the app died (PRD section 6.2, section 14 case 1, AC-05).
	ListByStatus(ctx context.Context, statuses ...recording.Status) ([]*recording.Recording, error)

	// Delete removes the row of r (PRD section 13.1, clear deletion semantics), guarded like Save: the row is deleted
	// WHERE id = ? AND version = r.Version(), and a recording that changed meanwhile (for example the watchdog or a
	// recovery moved it out of a deletable status) or is gone yields shared.ErrVersionConflict and nothing is deleted.
	// r is the recording loaded INSIDE the Tx.Do that deletes it, after the aggregate allowed the deletion in that
	// same transaction (see RecordingTranscripts for the whole flow). The transcripts of the recording are removed
	// explicitly through RecordingTranscripts.DeleteByRecording in the same Tx.Do, and the audio files through
	// ArtifactStore before that. The use case does not rely on the DB-level ON DELETE CASCADE of
	// transcripts.recording_id: the cascade stays in the schema only as a safety net behind PRAGMA foreign_keys.
	Delete(ctx context.Context, r *recording.Recording) error
}

// RecordingTranscripts lets the deletion of a recording find and remove the transcripts that belong to it
// (PRD section 13.1: deleting a recording deletes its transcripts and derived artifacts). Implemented by the sqlite
// adapter.
//
// Delete flow (ADR-0004(a) allows this second two-aggregate transaction because deleting a parent with its dependents
// has no cross-aggregate invariant to break, and a partial commit would leave orphans). The order is fixed so that a
// crash in the middle is repaired by repeating the request:
//  1. Decide first. Load the recording and ask the aggregate whether deletion is allowed (TASK-013 adds the method;
//     it refuses while the status is STARTING, RECORDING, STOPPING or FINALIZING, PRD section 6.2). The rule lives
//     in the aggregate, not in the use case (ARCH-22). If it refuses, nothing is touched: no file is deleted.
//  2. List the files (ListArtifactsByRecording) and delete them through ArtifactStore (idempotent, outside any
//     transaction because it is file I/O).
//  3. In one Tx.Do: reload the recording, ask the aggregate again (the guard is re-checked inside the transaction),
//     then DeleteByRecording and RecordingRepository.Delete(r) with the loaded version. A version conflict or a
//     refusal here rolls everything back and fails the request. The window between step 1 and step 3 is small and
//     only a status change after step 1 can open it (for example a recovery restarting FINALIZING); a crash or
//     failure after step 2 leaves rows that point at missing files, which a repeated deletion cleans up.
type RecordingTranscripts interface {
	// ListArtifactsByRecording returns the references of the files that belong to the transcripts of the recording
	// (PRD section 13.1), so that the use case can delete them. A recording without transcripts yields an empty list.
	ListArtifactsByRecording(ctx context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error)

	// DeleteByRecording removes the transcript rows of the recording (PRD section 13.1). It is idempotent: with no
	// transcripts it does nothing and returns nil. Assignments that referenced a transcript keep their data and lose
	// only the reference (transcript_id becomes NULL).
	DeleteByRecording(ctx context.Context, id shared.RecordingID) error
}

// Recorder captures system audio (WASAPI loopback) into chunk files, one track per recording
// (PRD sections 4.3, 6.2 and 6.3, AC-24). Implemented by a Windows adapter (TASK-017) and by a fake
// elsewhere. Audio never leaves the machine (AC-06).
//
// The adapter writes below the artifact root it was wired with; references keep the core free of OS paths.
//
// Every method must return soon after its ctx ends: the use case bounds the shutdown steps (Stop) with a timeout and
// cancels Wait and Finalize on a forced Close, and Close waits for them to return.
type Recorder interface {
	// Start begins capturing source for the recording id and writes chunk files into the directory chunks
	// (PRD section 6.3: chunked writes, a crash loses at most the latest chunk). It returns once capture runs.
	// ctx bounds only the start: cancelling it aborts a start in progress (the user stopped meanwhile), never a
	// capture that already runs.
	//
	// The whole computer is everything it plays except this app (ADR-0002 Decision 1). An app is process loopback of
	// its process tree (PRD section 6.3, TASK-059): when the app restarts during the recording, its new instance is
	// found again by the executable name and the capture goes on, with silence for the time between. If the app does
	// not run at the start, Start fails with an error wrapping recording.ErrSourceNotRunning.
	Start(ctx context.Context, id shared.RecordingID, chunks shared.ArtifactRef, source values.Source) error

	// Stop ends the capture of id and flushes the last chunk (PRD section 6.2: STOPPING).
	Stop(ctx context.Context, id shared.RecordingID) error

	// Wait blocks until the capture of id has ended. It returns nil after a Stop and an error if the capture
	// ended by itself, for example the output device changed or failed, so that the use case can move the
	// recording to FAILED or RECOVERY_REQUIRED (PRD section 6.2). Cancelling ctx stops waiting, not capturing.
	Wait(ctx context.Context, id shared.RecordingID) error

	// Finalize assembles the readable chunks in the directory chunks, in name order, into the single file output and
	// returns its duration (PRD section 6.2: FINALIZING, recovery after a restart). A chunk that cannot be read as
	// audio, typically the last one cut short by a crash (PRD section 6.3), is skipped and left on disk. Without a
	// readable chunk (none at all, or only broken ones) it returns an error wrapping recording.ErrNoAudio, after which
	// the recording fails without retries; any other error counts as transient. It is idempotent (output is
	// overwritten) and does not need a running capture.
	Finalize(ctx context.Context, chunks, output shared.ArtifactRef) (time.Duration, error)
}

// ArtifactStore is the access of this use case to recording files. All file access goes through
// the Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// List returns the direct children of the directory dir sorted by name, for example the chunk files
	// of an interrupted recording (PRD section 6.2). A missing directory yields shared.ErrNotFound.
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)

	// Delete removes a file or a whole directory tree. Deleting what is already gone is not an error
	// (PRD section 13.1: deletion semantics, idempotent). The files of a recording are its chunk directory and final
	// track (references held by the recording aggregate) and the transcript files listed by RecordingTranscripts.
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// CompletionListener is told that a recording was stored COMPLETED with its final track, so that the app enqueues its
// transcription (PRD sections 6.2 and 7.1). The app wires the transcription use case to it.
type CompletionListener interface {
	// RecordingCompleted is called after the COMPLETED state was saved: at the end of a stop, of the hard cap, of a
	// capture that ended by itself and of the startup recovery (PRD section 6.2). A crash between the save and the call
	// loses the call; the app's startup sweep enqueues what was missed, so delivery is at least once and the
	// implementation must be idempotent by id. An error is only logged: the recording stays COMPLETED.
	RecordingCompleted(ctx context.Context, id shared.RecordingID) error
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

// Sleeper waits on the same time source as Clock (ARCH-24): the wait for the hard cap of a recording
// (PRD section 6.2, AC-04). Implemented by the clock adapter and by a controllable fake in tests.
type Sleeper interface {
	// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error (PRD section 6.2).
	// A non-positive d returns at once.
	Sleep(ctx context.Context, d time.Duration) error
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 6.2). Implemented by an adapter and by a
// deterministic fake in tests.
type IDGenerator interface {
	// NewRecordingID returns a fresh recording identifier (PRD section 6.2).
	NewRecordingID() shared.RecordingID
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
