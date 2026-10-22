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
	// LessonTitle is the title the recording kept of its lesson at the start (TASK-088), else the title of the lesson in
	// the schedule (imported text, shown as plain text only); empty without a lesson or when a recording without a
	// snapshot lost its lesson.
	LessonTitle string
	// LessonSubject, LessonKind (a short code such as "lecture") and LessonTeacher come from the snapshot kept at the
	// start; empty if unknown. Imported text, shown as plain text only.
	LessonSubject string
	LessonKind    string
	LessonTeacher string
	Status        recording.Status
	StartedAt     time.Time
	StoppedAt     time.Time // zero while capturing or if unknown
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
	Stage   Stage
	Step    Step // the step a queued or failed chain stopped at
	Percent int  // progress of the transcription or of the lecture text, 0..100
	// PartsDone and Parts are the progress of the lecture text by parts (TASK-090), "3 of 12"; Parts is 0 when
	// unknown (the job has not reported yet).
	PartsDone   int
	Parts       int
	QueueReason QueueReason // why a queued chain waits
	ErrorCode   ErrorCode   // what failed
	JobError    string      // last error of the job the stage refers to (failed or retrying); sanitized by the job
	Action      Action      // what the screen offers for this row besides Stop
	ActionJob   shared.JobID

	// Retry is the post-processing work whose job failed or was cancelled and that the row may start again
	// («Повторить», TASK-086), also when Action is the connections page; "" when there is none.
	Retry Work
	// CanRedoSummary: a summary was made and its job is not pending or running, so «Сделать конспект заново» may run.
	CanRedoSummary bool
	// LectureTextReady: the lecture text of the current transcript is stored (TASK-090), so the row offers it instead
	// of the raw transcript; the transcript itself is kept (it feeds the retries).
	LectureTextReady bool
}

// Work is a post-processing work of a recording (TASK-086); the values are those of homeworkflow.Work.
type Work string

// Works.
const (
	WorkSummary     Work = "summary"
	WorkHomework    Work = "homework"
	WorkLectureText Work = "lectureText"
)

// Stage of the chain, in its order.
type Stage string

// Stages.
const (
	StageRecording    Stage = "recording"    // the capture runs
	StageSaving       Stage = "saving"       // the track is being stopped, assembled or recovered
	StageQueued       Stage = "queued"       // the next step waits; QueueReason says why
	StageTranscribing Stage = "transcribing" // speech recognition runs
	StageSummarizing  Stage = "summarizing"  // the summary and the homework are being made
	StageLectureText  Stage = "lectureText"  // the lecture text is being made, after the summary and the homework
	StageDone         Stage = "done"         // the summary is ready
	StageFailed       Stage = "failed"       // a step failed; ErrorCode says which
)

// Step is a processing step of the chain.
type Step string

// Steps.
const (
	StepTranscription Step = "transcription"
	StepSummary       Step = "summary"     // the summary and the homework search
	StepLectureText   Step = "lectureText" // the lecture text (TASK-090)
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
	ErrorLectureTextFailed   ErrorCode = "lectureTextFailed"
	// ErrorSilentRecording: the track is silent all or almost all the time (TASK-082), so it is not transcribed; the
	// row offers ActionCheckSound.
	ErrorSilentRecording ErrorCode = "silentRecording"
	// ErrorNoSpeech: the transcript holds no speech, only whisper's marks such as "[BLANK_AUDIO]" (TASK-110), so
	// nothing is post-processed; the row offers ActionCheckSound and no retry.
	ErrorNoSpeech ErrorCode = "noSpeech"
)

// Action is what the screen offers for a row.
type Action string

// Actions.
const (
	ActionRunNow          Action = "runNow"          // start the waiting job ActionJob at once (AC-22)
	ActionOpenModels      Action = "openModels"      // the speech models page
	ActionOpenConnections Action = "openConnections" // the connections page (Hub key, quota)
	ActionCheckSound      Action = "checkSound"      // the sound check
	// ActionRetry starts the failed post-processing work Chain.Retry again with a new job (TASK-086).
	ActionRetry Action = "retry"
)

// JobState is what the agent's job_status shows of a job (TASK-117): no error text, payload or names.
type JobState struct {
	ID      shared.JobID
	Type    job.Type
	Status  job.Status
	Percent int
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

	// The assignment of a homework draft job (TASK-094): the id opens it, the title and the subject name the row
	// («ДЗ: <задание> · <предмет>»). Empty for other jobs; the title and subject also when it is gone.
	AssignmentID      shared.AssignmentID
	AssignmentTitle   string
	AssignmentSubject string
}
