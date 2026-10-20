package activity

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// RecordingHistory lists stored recordings for the recordings screen (PRD section 6.2). Implemented by the sqlite
// adapter.
type RecordingHistory interface {
	// ListActivity returns every non-terminal recording plus at most maxTerminal of the newest terminal ones, newest
	// first by creation time, then id; maxTerminal below 1 adds no terminal recording. Rows that cannot be read are
	// left out and their ids returned in unreadable; only other failures are errors.
	ListActivity(ctx context.Context, maxTerminal int) (recs []*recording.Recording, unreadable []shared.RecordingID, err error)
}

// JobHistory lists stored jobs for the jobs screen (PRD section 7.2). Implemented by the sqlite adapter.
type JobHistory interface {
	// ListActivity returns every non-terminal job (so "Run now" of a waiting job stays reachable, AC-22) plus at most
	// maxTerminal of the newest terminal ones, newest first by creation time, then id; maxTerminal below 1 adds no
	// terminal job. Jobs of the hidden types are left out of both parts and do not count against maxTerminal. Rows
	// that cannot be read are left out and their ids returned in unreadable; only other failures are errors.
	ListActivity(ctx context.Context, maxTerminal int, hidden []job.Type) (jobs []*job.Job, unreadable []shared.JobID, err error)

	// ListForRecordings returns the jobs that process the recordings, by recording (TASK-076): the jobs of
	// byRecording types whose payload names the recording ("recording_id") and the jobs of byTranscript types whose
	// payload names one of its transcripts ("transcript_id"). Rows that cannot be read are left out and their ids
	// returned in unreadable; only other failures are errors.
	ListForRecordings(ctx context.Context, recs []shared.RecordingID, byRecording, byTranscript []job.Type) (
		jobs map[shared.RecordingID][]*job.Job, unreadable []shared.JobID, err error)
}

// Lessons reads the lesson of a recording for its title (PRD section 6.1). Implemented by the sqlite adapter.
type Lessons interface {
	// Get returns a live lesson; shared.ErrNotFound when it is missing or deleted from the schedule.
	Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error)
}

// TranscriptionTimes estimates how long recognising audio with an installed model takes in the current load mode
// (PRD section 8; TASK-084). The app wires the recognition speed use case.
type TranscriptionTimes interface {
	// TranscriptionTime returns the processing time of audio; ok is false unless the speed was measured on this
	// computer (PRD section 8; TASK-111).
	TranscriptionTime(ctx context.Context, model shared.SttModelID, audio time.Duration) (d time.Duration, ok bool, err error)
}

// Clock supplies the current time for the time left of a running transcription (ARCH-24; PRD section 7.3).
type Clock interface {
	// Now returns the current time (PRD section 7.3).
	Now() time.Time
}

// DraftAssignments names the assignment of a homework draft job for its row (PRD sections 7.2 and 10, TASK-094). The
// app wires it over the homework use case; it is optional (WithDraftAssignments).
type DraftAssignments interface {
	// DraftAssignment returns the title and the subject the assignment shows (PRD section 10); a missing assignment
	// wraps shared.ErrNotFound.
	DraftAssignment(ctx context.Context, id shared.AssignmentID) (title, subject string, err error)
}
