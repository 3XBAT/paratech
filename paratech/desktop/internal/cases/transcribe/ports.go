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
	"paratech/desktop/internal/domain/transcription/values"
)

// STTEngine runs local speech recognition (PRD sections 4.3 and 8, AC-06). Implemented by the whispercli adapter
// (TASK-024) which starts the whisper-cli sidecar as a separate CPU-only process for each call; a fake serves tests.
// There is no cloud implementation and there must never be one.
//
// The adapter reads and writes below the artifact root it was wired with; references keep the core free of OS paths.
type STTEngine interface {
	// Transcribe recognises the audio file audio with the model file model and writes the transcript document
	// (transcript.Document) to output, atomically and only on success. progress, if not nil, is called with
	// increasing percentages from 0 to 100, on a goroutine of the adapter, never concurrently and never after
	// Transcribe returns (PRD section 7.3: persistent progress). Cancelling ctx kills the process and leaves no
	// partial output behind (PRD section 4.3). A missing input file yields shared.ErrNotFound.
	//
	// segment, if not nil, is called with each segment as soon as the engine recognised it, in order, with the same
	// guarantees as progress (TASK-079: live transcript). The segments are a preview; the document is the result.
	//
	// load is the load mode of this run (TASK-084): the engine applies the threads, priority and CPU cap that
	// loadpolicy gives for it. A change of the setting applies from the next run.
	Transcribe(ctx context.Context, audio, model, output shared.ArtifactRef, load shared.RecognitionLoad,
		progress func(percent int), segment func(transcript.Segment)) error
}

// SettingsReader reads the load mode of speech recognition at the start of a run (PRD section 8; TASK-084).
// Implemented by the sqlite settings repository.
type SettingsReader interface {
	// Load returns the stored settings, or the defaults while nothing is stored (PRD section 8).
	Load(ctx context.Context) (*settings.Settings, error)
}

// SpeedStore keeps the recognition speed measured per installed model and load mode on this computer, which refines
// the time estimates of the pages (PRD section 8; TASK-084). Implemented by the sqlite adapter.
type SpeedStore interface {
	// Speed returns the measured speed; the zero value when nothing is measured (PRD section 8).
	Speed(ctx context.Context, key values.SpeedKey) (values.MeasuredSpeed, error)

	// SaveSpeed replaces the speed (PRD section 8). A model that is no longer installed yields an error wrapping
	// shared.ErrConflict.
	SaveSpeed(ctx context.Context, key values.SpeedKey, s values.MeasuredSpeed, now time.Time) error
}

// LiveParts stores what the engine recognised so far, so the transcript viewer shows it while the engine runs and
// after a restart (PRD sections 7.3 and 8; TASK-079). Implemented by the sqlite adapter.
type LiveParts interface {
	// SaveParts stores the parts of the recording in one transaction, replacing a stored part with the same number
	// (PRD section 7.3: a rerun of the engine numbers from 0 again).
	SaveParts(ctx context.Context, rec shared.RecordingID, parts []transcript.Part) error

	// DeleteParts removes every part of the recording once its transcript is stored (PRD section 8); none is not an
	// error.
	DeleteParts(ctx context.Context, rec shared.RecordingID) error
}

// TranscriptRepository stores the transcript aggregate in the local database (PRD sections 5.1 and 8, AC-08).
// Implemented by the sqlite adapter.
type TranscriptRepository interface {
	// Get loads a transcript (PRD section 8). It returns an error wrapping shared.ErrNotFound if there is none.
	Get(ctx context.Context, id shared.TranscriptID) (*transcript.Transcript, error)

	// Save inserts or updates a transcript with the pinned model name and version (PRD section 8, AC-08). It is the
	// version compare-and-swap as defined in ADR-0004(d): Version() == 0 is a new aggregate and is
	// INSERTed with version 1; Version() >= 1 is UPDATEd WHERE id = ? AND version = Version(), and a row that changed or
	// was purged meanwhile yields shared.ErrVersionConflict (never an insert). An insert of an id that exists already
	// also yields shared.ErrVersionConflict. On success the adapter calls Persisted(newVersion) on the aggregate, so
	// saving the same instance again works.
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

// ArtifactStore is the read access of this use case to transcript documents. All file access goes through the
// Storage Manager (PRD sections 4.1 and 5.2, hard rule 5). Implemented by the filestore adapter.
type ArtifactStore interface {
	// Open returns a reader of the artifact; the caller closes it. A missing artifact yields shared.ErrNotFound
	// (PRD section 5.2).
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
}

// EventPublisher hands the facts of this use case to the app, which schedules post-processing (PRD section 7.1). The
// app wires it; this use case does not do it itself.
type EventPublisher interface {
	// TranscriptCreated is called after the transcript is stored and before the STT job completes (PRD section 7.3).
	// Delivery is at least once: a rerun of the job after a crash or a failed call publishes the same event again, so
	// the implementation is idempotent by e.Transcript (for example a follow-up job whose idempotency key is derived
	// from it). An error fails the attempt, which is retried.
	TranscriptCreated(ctx context.Context, e transcript.Created) error
}

// Clock supplies the current time (ARCH-24; PRD section 8). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time (PRD section 8: transcript timestamps).
	Now() time.Time
}

// ModelResolver picks the model a new STT job pins (PRD section 8, AC-08). The app bridges it to the model catalog.
type ModelResolver interface {
	// ResolveForJob returns the installed model to pin: requested if set, otherwise the default, otherwise the only
	// installed model (PRD section 8). It is called inside the Tx.Do that enqueues the job, so that no removal slips in
	// between. Nothing installed wraps modelselection.ErrModelNotInstalled; several models without a default wrap
	// modelselection.ErrNoDefaultModel.
	ResolveForJob(ctx context.Context, requested shared.SttModelID) (shared.SttModelID, error)
}

// JobEnqueuer creates the STT job of a completed recording (PRD sections 7.1 and 7.3). Implemented by the sqlite
// adapter.
type JobEnqueuer interface {
	// Enqueue stores a new job. If a job with the same idempotency key exists, nothing is created and the ID of the
	// existing job is returned with created == false (PRD section 7.3, section 14 case 5).
	Enqueue(ctx context.Context, j *job.Job) (id shared.JobID, created bool, err error)

	// HasKeyPrefix reports whether a job of type typ in any status has an idempotency key that starts with prefix
	// (PRD section 7.3), for example any STT job of one recording whatever model it pins.
	HasKeyPrefix(ctx context.Context, typ job.Type, prefix string) (bool, error)
}

// PendingRecordings finds the recordings that the startup sweep hands to the enqueue again (PRD sections 6.2 and
// 7.1). Implemented by the sqlite adapter.
type PendingRecordings interface {
	// ListUntranscribed returns, in id order, up to limit ids greater than after of COMPLETED recordings with a final
	// track and no transcript (PRD section 8). Paging by after keeps the memory bounded.
	ListUntranscribed(ctx context.Context, after shared.RecordingID, limit int) ([]shared.RecordingID, error)
}

// IDGenerator supplies identifiers (ARCH-24; the domain never generates them; PRD section 7.3). Implemented by an
// adapter and by a deterministic fake in tests.
type IDGenerator interface {
	// NewJobID returns a fresh job identifier (PRD section 7.3).
	NewJobID() shared.JobID
}

// Tx is the unit-of-work port (ARCH-23; PRD sections 5.1 and 7.3: durable state changes). The enqueue reads the
// recording, picks the model and stores the job in one Tx.Do, so a model removal cannot slip between the choice and
// the pin (PRD section 14 case 11). Implemented by the sqlite adapter.
type Tx interface {
	// Do runs fn in one write transaction and commits if fn returns nil, otherwise it rolls back and returns the
	// error (PRD section 7.3). Repositories called with the context passed to fn take part in the transaction; a Do
	// called with that context joins it. fn performs no external I/O.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
