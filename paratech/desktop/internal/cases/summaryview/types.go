package summaryview

import (
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

// State is where a post-processing step (the summary, the homework search or the lecture text) of a recording stands.
type State string

// States of a step.
const (
	// StateNone: nothing to make yet (the recording is not transcribed) or nothing was made.
	StateNone State = "none"
	// StateWaiting: the job waits; Step.Wait says why.
	StateWaiting State = "waiting"
	// StateRunning: the job runs.
	StateRunning State = "running"
	// StateDone: the job finished; a result kept from before counts as done too.
	StateDone State = "done"
	// StateFailed: the job failed or was cancelled; a result made before stays visible.
	StateFailed State = "failed"
)

// WaitReason says why a waiting step waits. The values are those of the queue reasons of the recordings screen.
type WaitReason string

// Wait reasons.
const (
	WaitInLine  WaitReason = "inLine"  // waits for its turn
	WaitRetry   WaitReason = "retry"   // failed for now and is retried later
	WaitPower   WaitReason = "power"   // waits for AC power
	WaitNetwork WaitReason = "network" // waits for the network
	WaitQuota   WaitReason = "quota"   // waits for the Hub quota
	// WaitServiceNotConfigured: the Hub has no address or key.
	WaitServiceNotConfigured WaitReason = "serviceNotConfigured"
	// WaitServiceUnavailable: the Hub did not serve the call for now; retried automatically.
	WaitServiceUnavailable WaitReason = "serviceUnavailable"
)

// Step is the state of the summary or of the homework search.
type Step struct {
	State State
	Wait  WaitReason // set for StateWaiting only
	// JobError is the last error of a failed or retrying job: a fixed text of the core, never model output.
	JobError string
}

// Summary is the summary of the recording and where its job stands.
type Summary struct {
	Step
	// Ready: Content holds a summary. It may be ready while a new run waits, runs or failed.
	Ready     bool
	Content   summary.Content // model output: plain text only
	UpdatedAt time.Time
}

// Assignment is a homework item found in the lesson. Title and Description may be model output: plain text only.
type Assignment struct {
	ID          shared.AssignmentID
	Title       string
	Description string
	Due         time.Time // zero: no deadline
	UserEdited  bool
}

// Homework is the homework found in the lesson and where its search stands.
type Homework struct {
	Step
	Assignments []Assignment
}

// LectureText is the lecture text of the recording (TASK-090) and where its job stands. StateNone with a transcript
// means it was never made (the transcript is older than the lecture text): CanStart offers to make it.
type LectureText struct {
	Step
	// Ready: Blocks hold the text. It may be ready while a new run waits, runs or failed.
	Ready     bool
	Blocks    []lecturetext.Block // model output: plain text only
	UpdatedAt time.Time
	// PartsDone of Parts are made, for a job that waits or runs; Parts is 0 when unknown.
	PartsDone int
	Parts     int
	// CanStart: the transcript has no lecture text job, so one may be started.
	CanStart bool
}

// View is the summary viewer of one recording.
type View struct {
	Recording shared.RecordingID
	Lesson    shared.LessonID // empty for an ad-hoc recording
	// Title is the lesson title, or "Запись <date>, <time>" for a recording without a lesson.
	Title     string
	StartedAt time.Time
	// Duration is the length of the final track; HasDuration is false until the recording is completed.
	Duration    time.Duration
	HasDuration bool
	// Transcribed: the recording has a transcript, so the summary and the homework can be made.
	Transcribed bool
	Summary     Summary
	Homework    Homework
	LectureText LectureText
}

// Deps are the ports the Service needs; Log is optional.
type Deps struct {
	Recordings   RecordingReader
	Lessons      LessonReader
	Transcripts  TranscriptFinder
	Summaries    SummaryReader
	LectureTexts LectureTextReader
	Assignments  AssignmentLister
	Jobs         JobFinder
	Writer       TextWriter
	Clock        Clock
	Log          *slog.Logger
}
