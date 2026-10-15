package activity

import (
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Recording is one row of the recordings screen.
type Recording struct {
	ID        shared.RecordingID
	LessonID  shared.LessonID // empty for an ad-hoc recording
	Status    recording.Status
	StartedAt time.Time
	StoppedAt time.Time // zero while capturing or if unknown
	// Duration is the length of the final track; HasDuration is false until the recording is COMPLETED.
	Duration    time.Duration
	HasDuration bool
	Problem     string // short reason of a failure or of an early end
	// Stoppable: the user may stop the recording (only a RECORDING one).
	Stoppable bool
}

// Job is one row of the jobs screen.
type Job struct {
	ID          shared.JobID
	Type        job.Type
	Status      job.Status
	Waiting     bool // one of WAITING_FOR_*: the status is the waiting reason (PRD section 7.2)
	Attempts    int
	MaxAttempts int
	Percent     int
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	NextRunAt   time.Time
	// RunNowOffered: the screen offers "Run now" (PRD section 7.4, AC-22).
	RunNowOffered bool
}
