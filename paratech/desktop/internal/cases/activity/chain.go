package activity

import (
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// recordingJobTypes are the job types of the chain (PRD section 7.1) whose payload names the recording.
func recordingJobTypes() []job.Type { return []job.Type{transcription.STTJobType} }

// transcriptJobTypes are the job types of the chain whose payload names a transcript of the recording.
func transcriptJobTypes() []job.Type {
	return []job.Type{values.SummarizeJobType, values.ExtractHomeworkJobType}
}

// chainJobTypes returns every job type of the chain. Their jobs are shown inside the row of their recording, never
// as rows of their own.
func chainJobTypes() []job.Type { return append(recordingJobTypes(), transcriptJobTypes()...) }

// chainOf derives the one status of the whole chain of a recording (TASK-076) from the recording and the jobs that
// process it: capture, saving, transcription, then the summary and the homework. Of several jobs of one type only
// the newest counts: an older one was replaced (for example a transcription with another model).
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
	if stt != nil {
		// Post-processing created before the newest transcription belongs to an older transcript.
		summary, homework = since(summary, stt), since(homework, stt)
	}
	if summary != nil || homework != nil {
		// The transcript exists: its post-processing decides.
		return postChain(summary, homework)
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
	return postChain(summary, homework)
}

// postChain is the chain once the transcript exists: the summary and the homework search; nil is a job not enqueued
// yet.
func postChain(summary, homework *job.Job) Chain {
	post := []struct {
		j    *job.Job
		code ErrorCode
	}{{summary, ErrorSummaryFailed}, {homework, ErrorHomeworkFailed}}
	for _, p := range post {
		if p.j != nil && failedJob(p.j) {
			return Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: p.code, JobError: p.j.LastError(), Action: ActionOpenConnections,
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
	return Chain{Stage: StageDone}
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
