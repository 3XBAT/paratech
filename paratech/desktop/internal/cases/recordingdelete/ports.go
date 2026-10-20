package recordingdelete

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// RecordingRepository stores the recording aggregate (PRD sections 6.2 and 13.1). Implemented by the sqlite adapter.
type RecordingRepository interface {
	// Get loads a recording, marked or not; an error wrapping shared.ErrNotFound if there is none (PRD section 6.2).
	Get(ctx context.Context, id shared.RecordingID) (*recording.Recording, error)

	// Save is the version compare-and-swap of ADR-0004(d); here it stores the deletion mark (PRD section 13.1).
	Save(ctx context.Context, r *recording.Recording) error

	// ListDeleting returns the recordings marked for deletion, oldest mark first, for the resume at the start (PRD
	// section 14 case 1).
	ListDeleting(ctx context.Context) ([]shared.RecordingID, error)
}

// DeletionStore reads what a recording holds and removes its rows (PRD section 13.1). Implemented by the sqlite
// adapter.
type DeletionStore interface {
	// Contents returns the transcripts, live parts, summaries, lecture texts and the assignments found in the
	// transcripts of the recording; Sound and Jobs are left zero (PRD section 13.1).
	Contents(ctx context.Context, id shared.RecordingID) (deletion.Contents, error)

	// Files returns the documents of the transcripts of the recording (PRD section 5.2).
	Files(ctx context.Context, id shared.RecordingID) ([]shared.ArtifactRef, error)

	// Purge removes the live parts, summaries, lecture texts and transcripts of r, the assignments with their drafts
	// (each leaves a deadline event removal at at), the finished jobs among jobs, and the row of r guarded by its
	// version (shared.ErrVersionConflict when it changed), all in the caller's transaction or its own (PRD section 13.1).
	Purge(ctx context.Context, r *recording.Recording, assignments []shared.AssignmentID, jobs []shared.JobID,
		at time.Time) error
}

// JobFinder finds the jobs that work for a recording (PRD section 7). Implemented by the sqlite adapter. Rows that
// cannot be read are left out and their ids returned.
type JobFinder interface {
	// ListForRecordings returns the jobs of byRecording types whose payload names a recording and of byTranscript
	// types whose payload names one of its transcripts, by recording (PRD section 7.1).
	ListForRecordings(ctx context.Context, recs []shared.RecordingID, byRecording, byTranscript []job.Type) (
		map[shared.RecordingID][]*job.Job, []shared.JobID, error)

	// ListForAssignments returns the jobs of the types whose payload names one of the assignments (PRD section 7.1).
	ListForAssignments(ctx context.Context, ids []shared.AssignmentID, types []job.Type) (
		[]*job.Job, []shared.JobID, error)
}

// JobCanceller cancels a job at the user's request (PRD section 7.2): a waiting one at once, a running one through its
// handler context. Implemented by the job runner. An error means "not now": the use case looks again.
type JobCanceller interface {
	// Cancel cancels the job id or refuses for now (PRD section 7.2).
	Cancel(ctx context.Context, id shared.JobID) error
}

// ArtifactStore deletes files through the Storage Manager (PRD section 5.2, hard rule 5). Implemented by the filestore
// adapter.
type ArtifactStore interface {
	// Delete removes a file or a directory tree; what is already gone is not an error (PRD section 13.1).
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

// RecoveryGuard tells whether the startup recovery of interrupted recordings is assembling a recording right now, so
// that its track is not written after the files were deleted. Implemented by the recording session.
type RecoveryGuard interface {
	// Recovering reports whether the recovery holds the recording id now (PRD section 6.2).
	Recovering(id shared.RecordingID) bool
}

// SyncRequester asks for a calendar sync, which removes the deadline events of the deleted assignments (PRD section
// 11.3). Implemented in the app over the calendar sync; it only logs, without a sign-in it does nothing.
type SyncRequester interface {
	// RequestSync asks for a sync run (PRD section 11.3).
	RequestSync(ctx context.Context)
}

// Clock supplies the current time (ARCH-24).
type Clock interface {
	// Now returns the current time (PRD section 13.1: the deletion mark).
	Now() time.Time
}

// Sleeper waits on the same time source as Clock, between the looks at the jobs being cancelled.
type Sleeper interface {
	// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error.
	Sleep(ctx context.Context, d time.Duration) error
}

// Tx is the unit-of-work port (ARCH-23). The mark changes one aggregate; the purge removes the recording with what
// was made from it, the exception of ADR-0018 (extending ADR-0004(a)). The sqlite adapter opens a write transaction
// up front (BEGIN IMMEDIATE), so a job cannot be claimed or enqueued between the check of the jobs and the purge.
// fn performs no external I/O.
type Tx interface {
	// Do runs fn in one write transaction (PRD section 7.3) and commits if fn returns nil.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
