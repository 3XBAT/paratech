package recordingdelete

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// Deps are the ports of the Service; Sync and Log are optional.
type Deps struct {
	Recordings RecordingRepository
	Store      DeletionStore
	Jobs       JobFinder
	Canceller  JobCanceller
	Artifacts  ArtifactStore
	Recovery   RecoveryGuard
	Clock      Clock
	Sleeper    Sleeper
	Tx         Tx
	Sync       SyncRequester
	Log        *slog.Logger
}

// Config tunes the Service. TrackExtension is the extension of final tracks (a track written just before a crash is
// deleted even when the recording never recorded it). Wait bounds how long a deletion waits for its jobs to stop
// (default DefaultWait), Poll how often it looks (default DefaultPoll).
type Config struct {
	TrackExtension string
	Wait           time.Duration
	Poll           time.Duration
}

// Defaults of Config.
const (
	DefaultWait = 20 * time.Second
	DefaultPoll = 200 * time.Millisecond
)

// MaxBatch bounds the recordings of one DeleteMany.
const MaxBatch = 100

// Refusal is why a recording cannot be deleted now; "" when it can.
type Refusal string

// Refusals of a Preview.
const (
	RefusalNone       Refusal = ""
	RefusalInProgress Refusal = "in_progress" // being captured or finalized: stop it first
	RefusalRecovering Refusal = "recovering"  // the startup recovery is assembling it
)

// Preview is what the confirmation shows: the recording and what its deletion removes. Contents.Assignments are the
// found homework with their classes (deletion.Assignment.Class).
type Preview struct {
	Recording shared.RecordingID
	Title     string // the lesson title (imported text), "" for an ad-hoc recording
	StartedAt time.Time
	Refusal   Refusal
	Contents  deletion.Contents
}

// Request deletes a recording with the found assignments the user chose.
type Request struct {
	Recording   shared.RecordingID
	Assignments []shared.AssignmentID
}

// Outcome is how a deletion ended.
type Outcome string

// Outcomes of a deletion.
const (
	// OutcomeDeleted: everything is gone.
	OutcomeDeleted Outcome = "deleted"
	// OutcomePending: the recording is marked and hidden, but a job did not stop in time or a file could not be
	// deleted; the next start finishes it.
	OutcomePending Outcome = "pending"
)

// Result is the outcome of one deletion. Deleted are the assignments deleted with the recording; Kept the found ones
// that stay without it (not chosen, or with email history). Both are empty unless the outcome is OutcomeDeleted.
type Result struct {
	Recording shared.RecordingID
	Outcome   Outcome
	Deleted   []shared.AssignmentID
	Kept      []shared.AssignmentID
}

// Failure is a recording of a batch that was not deleted, with why.
type Failure struct {
	Recording shared.RecordingID
	Err       error
}

// Batch is the outcome of DeleteMany.
type Batch struct {
	Results  []Result
	Failures []Failure
}
