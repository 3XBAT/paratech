package activity

import (
	"strings"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// recordingJobTypes are the job types of the chain (PRD section 7.1) whose payload names the recording.
func recordingJobTypes() []job.Type { return []job.Type{transcription.STTJobType} }

// transcriptJobTypes are the job types of the chain whose payload names a transcript of the recording.
func transcriptJobTypes() []job.Type {
	return []job.Type{values.SummarizeJobType, values.ExtractHomeworkJobType, values.LectureTextJobType}
}

// chainJobTypes returns every job type of the chain. Their jobs are shown inside the row of their recording, never
// as rows of their own.
func chainJobTypes() []job.Type { return append(recordingJobTypes(), transcriptJobTypes()...) }

// chainOf derives the one status of the whole chain of a recording (TASK-076) from the recording and the jobs that
// process it: capture, saving, transcription, then the summary and the homework, then the lecture text (TASK-090). Of
// several jobs of one type only the newest counts: an older one was replaced (for example a transcription with another
// model).
func chainOf(r *recording.Recording, jobs []*job.Job) Chain {
	switch r.Status() {
	case recording.StatusCompleted:
		if r.Silent() {
			// TASK-082: nothing to transcribe; a job enqueued before cannot succeed either.
			return Chain{Stage: StageFailed, ErrorCode: ErrorSilentRecording, Action: ActionCheckSound}
		}
		// processed: the jobs decide below
	case recording.StatusFailed:
		return Chain{Stage: StageFailed, ErrorCode: ErrorRecordingFailed, Action: ActionCheckSound}
	case recording.StatusStarting, recording.StatusRecording:
		return Chain{Stage: StageRecording}
	default: // stopping, finalizing, recovering the track
		return Chain{Stage: StageSaving}
	}

	stt := newest(jobs, transcription.STTJobType)
	summary, homework := newest(jobs, values.SummarizeJobType), newest(jobs, values.ExtractHomeworkJobType)
	lecture := newest(jobs, values.LectureTextJobType)
	if stt != nil {
		// Post-processing created before the newest transcription belongs to an older transcript.
		summary, homework, lecture = since(summary, stt), since(homework, stt), since(lecture, stt)
	}
	if summary != nil || homework != nil || lecture != nil {
		// The transcript exists: its post-processing decides.
		return withStored(postChain(summary, homework, lecture), jobs, stt)
	}
	if stt == nil {
		// The STT job is enqueued right after the recording completes, or once a speech model is installed.
		return Chain{Stage: StageQueued, Step: StepTranscription, QueueReason: QueueInLine}
	}
	switch {
	case stt.Status() == job.StatusRunning:
		return Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: stt.Progress().Percent()}
	case failedJob(stt):
		return Chain{
			Stage: StageFailed, Step: StepTranscription, ErrorCode: ErrorTranscriptionFailed, JobError: stt.LastError(),
			Action: ActionOpenModels,
		}
	case stt.Status() != job.StatusCompleted:
		return queued(StepTranscription, stt)
	}
	return withStored(postChain(summary, homework, lecture), jobs, stt)
}

// withStored sets what is stored of the current transcript. CanRedoSummary: the newest summary job has finished and
// one of its summary jobs completed, so a summary is stored (a failed redo keeps the one before). LectureTextReady: a
// lecture text job of the transcript completed; a later run replaces the text only when it completes, so a newer job
// that runs or failed keeps it ready.
func withStored(c Chain, jobs []*job.Job, stt *job.Job) Chain {
	c.LectureTextReady = completedSince(jobs, values.LectureTextJobType, stt)
	last := newest(jobs, values.SummarizeJobType)
	if stt != nil {
		last = since(last, stt)
	}
	if last != nil && last.Status().IsTerminal() {
		c.CanRedoSummary = completedSince(jobs, values.SummarizeJobType, stt)
	}
	return c
}

// completedSince reports whether a job of the type completed, created no earlier than stt (any one when stt is nil):
// one of the current transcript.
func completedSince(jobs []*job.Job, typ job.Type, stt *job.Job) bool {
	for _, j := range jobs {
		if j.Type() == typ && j.Status() == job.StatusCompleted && (stt == nil || since(j, stt) != nil) {
			return true
		}
	}
	return false
}

// postChain is the chain once the transcript exists: the summary and the homework search, then the lecture text; nil
// is a job not enqueued yet.
func postChain(summary, homework, lecture *job.Job) Chain {
	post := []struct {
		j    *job.Job
		code ErrorCode
		work Work
	}{{summary, ErrorSummaryFailed, WorkSummary}, {homework, ErrorHomeworkFailed, WorkHomework}}
	for _, p := range post {
		if p.j != nil && failedJob(p.j) {
			return Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: p.code, JobError: p.j.LastError(), Action: failedAction(p.j),
				Retry: p.work,
			}
		}
	}
	for _, p := range post {
		if p.j != nil && p.j.Status() == job.StatusRunning {
			return Chain{Stage: StageSummarizing, Step: StepSummary}
		}
	}
	for _, p := range post {
		switch {
		case p.j == nil: // enqueued right after the transcript is stored
			return Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine}
		case p.j.Status() != job.StatusCompleted:
			return queued(StepSummary, p.j)
		}
	}
	return lectureChain(lecture)
}

// lectureChain is the chain once the summary and the homework are done: the lecture text, with its part progress. A
// transcript made before the lecture text existed has no job: the chain is done, the summary page offers to make it.
func lectureChain(j *job.Job) Chain {
	switch {
	case j == nil || j.Status() == job.StatusCompleted:
		return Chain{Stage: StageDone}
	case failedJob(j):
		return Chain{
			Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorLectureTextFailed, JobError: j.LastError(),
			Action: failedAction(j), Retry: WorkLectureText,
		}
	}
	var c Chain
	if j.Status() == job.StatusRunning {
		c = Chain{Stage: StageLectureText, Step: StepLectureText, Percent: j.Progress().Percent()}
	} else {
		c = queued(StepLectureText, j)
	}
	if done, parts, ok := values.ParseLectureTextCheckpoint(j.Progress().Checkpoint()); ok {
		c.PartsDone, c.Parts = done, parts
	}
	return c
}

// failedAction is what the row offers for a failed post-processing job. The connections page helps only when the LLM
// call or the Hub failed (connection, key, quota); an invalid or cut-off answer and the other failures are retried
// with a new job (TASK-086).
func failedAction(j *job.Job) Action {
	if j.Cause() != job.CauseNone || strings.HasSuffix(j.LastError(), values.LLMFailedMessage) {
		return ActionOpenConnections
	}
	return ActionRetry
}

// queued is the chain of a step whose job waits; the job's status and the cause it recorded are the reason. A Hub
// that is not set up parks the job in WAITING_FOR_NETWORK too (TASK-083), so the cause comes first.
func queued(step Step, j *job.Job) Chain {
	c := Chain{Stage: StageQueued, Step: step, JobError: j.LastError()}
	switch {
	case j.Cause() == job.CauseServiceNotConfigured:
		c.QueueReason, c.Action = QueueServiceNotConfigured, ActionOpenConnections
		return c
	case j.Cause() == job.CauseServiceUnavailable:
		c.QueueReason = QueueServiceUnavailable
		return c
	}
	switch j.Status() {
	case job.StatusRetryWait:
		c.QueueReason = QueueRetry
	case job.StatusWaitingForPower:
		c.QueueReason = QueuePower
	case job.StatusWaitingForNetwork:
		c.QueueReason = QueueNetwork
	case job.StatusWaitingForQuota:
		c.QueueReason, c.Action = QueueQuota, ActionOpenConnections
	default:
		c.QueueReason = QueueInLine
	}
	if j.Status().OffersRunNow() {
		c.Action, c.ActionJob = ActionRunNow, j.ID()
	}
	return c
}

func failedJob(j *job.Job) bool {
	return j.Status() == job.StatusFailed || j.Status() == job.StatusCancelled
}

// since returns j unless it was created before ref; nil stays nil.
func since(j, ref *job.Job) *job.Job {
	if j == nil || j.CreatedAt().Before(ref.CreatedAt()) {
		return nil
	}
	return j
}

// newest returns the latest created job of the type, or nil.
func newest(jobs []*job.Job, typ job.Type) *job.Job {
	var out *job.Job
	for _, j := range jobs {
		if j.Type() != typ {
			continue
		}
		if out == nil || j.CreatedAt().After(out.CreatedAt()) ||
			(j.CreatedAt().Equal(out.CreatedAt()) && j.ID() > out.ID()) {
			out = j
		}
	}
	return out
}
