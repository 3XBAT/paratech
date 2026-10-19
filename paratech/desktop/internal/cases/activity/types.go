package activity

import (
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// Recording is one row of the recordings screen: a recording with the status of its whole chain (TASK-076).
type Recording struct {
	ID       shared.RecordingID
	LessonID shared.LessonID // empty for an ad-hoc recording
	// LessonTitle is the title of the lesson (imported text, shown as plain text only); empty without a lesson or
	// when the lesson is gone from the schedule.
	LessonTitle string
	Status      recording.Status
	StartedAt   time.Time
	StoppedAt   time.Time // zero while capturing or if unknown
	// Duration is the length of the final track; HasDuration is false until the recording is COMPLETED.
	Duration    time.Duration
	HasDuration bool
	Problem     string // short reason of a failure or of an early end
	// Stoppable: the user may stop the recording (only a RECORDING one).
	Stoppable bool
	Chain     Chain

	// Remaining is the approximate time left of a queued or running transcription (TASK-084); HasRemaining is false
	// when there is none or no estimate.
	Remaining    time.Duration
	HasRemaining bool
}

// Chain is the one status of everything that happens to a recording (TASK-076): the screen shows it instead of the
// rows of the jobs that process the recording.
type Chain struct {
	Stage       Stage
	Step        Step        // the step a queued or failed chain stopped at
	Percent     int         // progress of the transcription, 0..100
	QueueReason QueueReason // why a queued chain waits
	ErrorCode   ErrorCode   // what failed
	JobError    string      // last error of the job the stage refers to (failed or retrying); sanitized by the job
	Action      Action      // what the screen offers for this row besides Stop
	ActionJob   shared.JobID
}

// Stage of the chain, in its order.
type Stage string

// Stages.
const (
	StageRecording    Stage = "recording"    // the capture runs
	StageSaving       Stage = "saving"       // the track is being stopped, assembled or recovered
	StageQueued       Stage = "queued"       // the next step waits; QueueReason says why
	StageTranscribing Stage = "transcribing" // speech recognition runs
	StageSummarizing  Stage = "summarizing"  // the summary and the homework are being made
	StageDone         Stage = "done"         // the summary is ready
	StageFailed       Stage = "failed"       // a step failed; ErrorCode says which
)

// Step is a processing step of the chain.
type Step string

// Steps.
const (
	StepTranscription Step = "transcription"
	StepSummary       Step = "summary" // the summary and the homework search
)

// QueueReason says why a step waits.
type QueueReason string

// Queue reasons.
const (
	QueueInLine  QueueReason = "inLine"  // waits for its turn
	QueueRetry   QueueReason = "retry"   // failed for now and is retried later
	QueuePower   QueueReason = "power"   // waits for AC power (PRD section 7.4)
	QueueNetwork QueueReason = "network" // waits for the network
	QueueQuota   QueueReason = "quota"   // waits for the Hub quota
	// QueueServiceNotConfigured: the Hub has no address or key; the job runs once the user sets it up (TASK-083).
	QueueServiceNotConfigured QueueReason = "serviceNotConfigured"
	// QueueServiceUnavailable: the Hub did not serve the call for now; the job is retried automatically (TASK-083).
	QueueServiceUnavailable QueueReason = "serviceUnavailable"
)

// ErrorCode says what failed.
type ErrorCode string

// Error codes.
const (
	ErrorRecordingFailed     ErrorCode = "recordingFailed"
	ErrorTranscriptionFailed ErrorCode = "transcriptionFailed"
	ErrorSummaryFailed       ErrorCode = "summaryFailed"
	ErrorHomeworkFailed      ErrorCode = "homeworkFailed"
)

// Action is what the screen offers for a row.
type Action string

// Actions.
const (
	ActionRunNow          Action = "runNow"          // start the waiting job ActionJob at once (AC-22)
	ActionOpenModels      Action = "openModels"      // the speech models page
	ActionOpenConnections Action = "openConnections" // the connections page (Hub key, quota)
	ActionCheckSound      Action = "checkSound"      // the sound check
)

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
