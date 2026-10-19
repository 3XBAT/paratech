package activity

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// recordingIn builds a recording that reached s by legal moves.
func recordingIn(t *testing.T, s recording.Status) *recording.Recording {
	t.Helper()
	r := newRecording(t, "r1")
	at := t0.Add(time.Second)
	steps := map[recording.Status][]func() error{
		recording.StatusStarting:   nil,
		recording.StatusRecording:  {func() error { return r.Begin(at) }},
		recording.StatusStopping:   {func() error { return r.Begin(at) }, func() error { return r.Stop(at.Add(time.Hour)) }},
		recording.StatusFinalizing: {func() error { return r.Begin(at) }, func() error { return r.Stop(at.Add(time.Hour)) }, func() error { return r.Finalize(at.Add(time.Hour)) }},
		recording.StatusRecoveryRequired: {func() error { return r.Begin(at) }, func() error {
			return r.RequireRecovery(at.Add(time.Hour), "stale heartbeat")
		}},
		recording.StatusFailed: {func() error { return r.Fail(at, "device lost") }},
		recording.StatusCompleted: {
			func() error { return r.Begin(at) }, func() error { return r.Stop(at.Add(time.Hour)) },
			func() error { return r.Finalize(at.Add(time.Hour)) },
			func() error {
				track, err := r.TrackRef("wav")
				if err != nil {
					return err
				}
				return r.Complete(at.Add(time.Hour+time.Second), track, time.Hour)
			},
		},
	}
	for _, step := range steps[s] {
		must(t, step())
	}
	if r.Status() != s {
		t.Fatalf("recording in %s, want %s", r.Status(), s)
	}
	return r
}

// jobIn builds a job of typ created at t0+created that reached status s.
func jobIn(t *testing.T, id string, typ job.Type, created time.Duration, s job.Status) *job.Job {
	t.Helper()
	at := t0.Add(created)
	j, err := job.New(job.Spec{ID: shared.JobID(id), Type: typ, Key: string(typ) + ":" + id, MaxAttempts: 3}, at)
	must(t, err)
	claim := func() error { return j.Claim(at.Add(time.Second)) }
	steps := map[job.Status][]func() error{
		job.StatusQueued:  nil,
		job.StatusRunning: {claim, func() error { return reportPercent(j, 42, at.Add(2*time.Second)) }},
		job.StatusCompleted: {claim, func() error {
			return j.Complete(at.Add(2 * time.Second))
		}},
		job.StatusRetryWait: {claim, func() error { return j.Retry(at.Add(2*time.Second), at.Add(time.Minute), "hub timeout") }},
		job.StatusFailed:    {claim, func() error { return j.Fail(at.Add(2*time.Second), "bad answer") }},
		job.StatusCancelled: {func() error { return j.Cancel(at.Add(time.Second)) }},
		job.StatusWaitingForNetwork: {claim, func() error {
			return j.WaitFor(job.StatusWaitingForNetwork, at.Add(2*time.Second))
		}},
		job.StatusWaitingForQuota: {claim, func() error {
			return j.WaitFor(job.StatusWaitingForQuota, at.Add(2*time.Second))
		}},
		job.StatusWaitingForPower: {func() error { return j.WaitFor(job.StatusWaitingForPower, at.Add(time.Second)) }},
	}
	for _, step := range steps[s] {
		must(t, step())
	}
	if j.Status() != s {
		t.Fatalf("job in %s, want %s", j.Status(), s)
	}
	return j
}

// Last errors of post-processing jobs (homeworkflow).
const (
	msgInvalid   = "post-processing: the LLM answer does not match the schema"
	msgTruncated = "post-processing: the LLM answer looks cut off at the output limit"
	msgLLM       = "post-processing: the LLM call failed"
)

// failed returns a builder of a job of typ created at t0+created that failed with msg.
func failed(id string, typ job.Type, created time.Duration, msg string) func(t *testing.T) *job.Job {
	return func(t *testing.T) *job.Job {
		t.Helper()
		j := jobIn(t, id, typ, created, job.StatusRunning)
		must(t, j.Fail(t0.Add(created+3*time.Second), msg))
		return j
	}
}

// jobWithCause builds a job of typ created at t0 that the runner parked (WAITING_FOR_NETWORK) or retried (RETRY_WAIT,
// FAILED with one attempt) with the cause it recorded (TASK-083).
func jobWithCause(t *testing.T, id string, typ job.Type, s job.Status, cause job.Cause) *job.Job {
	t.Helper()
	attempts := 3
	if s == job.StatusFailed {
		attempts = 1
	}
	j, err := job.New(job.Spec{ID: shared.JobID(id), Type: typ, Key: string(typ) + ":" + id, MaxAttempts: attempts}, t0)
	must(t, err)
	must(t, j.Claim(t0.Add(time.Second)))
	at := t0.Add(2 * time.Second)
	if s == job.StatusWaitingForNetwork {
		must(t, j.WaitForCause(s, cause, at))
	} else {
		must(t, j.RetryCause(at, at.Add(time.Minute), "hub: hub unavailable", cause))
	}
	if j.Status() != s || j.Cause() != cause {
		t.Fatalf("job in %s %q, want %s %q", j.Status(), j.Cause(), s, cause)
	}
	return j
}

func reportPercent(j *job.Job, percent int, at time.Time) error {
	p, err := job.NewProgress(percent, "chunk=1")
	if err != nil {
		return err
	}
	return j.ReportProgress(p, at)
}

// One status of the whole chain (TASK-076): Идёт запись -> Сохраняется -> Расшифровка N % / В очереди -> Краткое
// содержание -> Конспект готов / Ошибка, with the reason and the action the screen offers.
func TestChainOf(t *testing.T) {
	const (
		stt = job.Type("stt")
		sum = job.Type("summarize")
		hw  = job.Type("extract_homework")
	)
	done := func(id string, typ job.Type) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobIn(t, id, typ, 0, job.StatusCompleted) }
	}
	in := func(id string, typ job.Type, s job.Status) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobIn(t, id, typ, 0, s) }
	}
	caused := func(id string, typ job.Type, s job.Status, c job.Cause) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobWithCause(t, id, typ, s, c) }
	}
	tests := []struct {
		name string
		rec  recording.Status
		jobs []func(t *testing.T) *job.Job
		want Chain
	}{
		{name: "starting", rec: recording.StatusStarting, want: Chain{Stage: StageRecording}},
		{name: "recording", rec: recording.StatusRecording, want: Chain{Stage: StageRecording}},
		{name: "stopping", rec: recording.StatusStopping, want: Chain{Stage: StageSaving}},
		{name: "finalizing", rec: recording.StatusFinalizing, want: Chain{Stage: StageSaving}},
		{name: "recovering", rec: recording.StatusRecoveryRequired, want: Chain{Stage: StageSaving}},
		{
			name: "recording failed", rec: recording.StatusFailed,
			want: Chain{Stage: StageFailed, ErrorCode: ErrorRecordingFailed, Action: ActionCheckSound},
		},
		{
			name: "completed without a transcription job", rec: recording.StatusCompleted,
			want: Chain{Stage: StageQueued, Step: StepTranscription, QueueReason: QueueInLine},
		},
		{
			name: "transcription queued", rec: recording.StatusCompleted, jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusQueued)},
			want: Chain{Stage: StageQueued, Step: StepTranscription, QueueReason: QueueInLine},
		},
		{
			name: "transcription waits for power", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusWaitingForPower)},
			want: Chain{
				Stage: StageQueued, Step: StepTranscription, QueueReason: QueuePower, Action: ActionRunNow, ActionJob: "s",
			},
		},
		{
			name: "transcription retries", rec: recording.StatusCompleted, jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusRetryWait)},
			want: Chain{Stage: StageQueued, Step: StepTranscription, QueueReason: QueueRetry, JobError: "hub timeout"},
		},
		{
			name: "transcription runs", rec: recording.StatusCompleted, jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusRunning)},
			want: Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: 42},
		},
		{
			name: "transcription failed", rec: recording.StatusCompleted, jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusFailed)},
			want: Chain{
				Stage: StageFailed, Step: StepTranscription, ErrorCode: ErrorTranscriptionFailed, JobError: "bad answer",
				Action: ActionOpenModels,
			},
		},
		{
			name: "transcription cancelled", rec: recording.StatusCompleted, jobs: []func(t *testing.T) *job.Job{in("s", stt, job.StatusCancelled)},
			want: Chain{Stage: StageFailed, Step: StepTranscription, ErrorCode: ErrorTranscriptionFailed, Action: ActionOpenModels},
		},
		{
			name: "the newest transcription counts", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				in("old", stt, job.StatusFailed),
				func(t *testing.T) *job.Job { return jobIn(t, "new", stt, time.Minute, job.StatusRunning) },
			},
			want: Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: 42},
		},
		{
			name: "transcribed, post-processing not enqueued yet", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt)},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine},
		},
		{
			name: "summary runs", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), in("a", sum, job.StatusRunning), in("h", hw, job.StatusQueued)},
			want: Chain{Stage: StageSummarizing, Step: StepSummary},
		},
		{
			name: "summary waits for the network", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), in("a", sum, job.StatusWaitingForNetwork), in("h", hw, job.StatusQueued)},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueNetwork},
		},
		{
			name: "summary service not configured", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), caused("a", sum, job.StatusWaitingForNetwork, job.CauseServiceNotConfigured),
				in("h", hw, job.StatusQueued)},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueServiceNotConfigured, Action: ActionOpenConnections},
		},
		{
			name: "homework service not configured", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum),
				caused("h", hw, job.StatusWaitingForNetwork, job.CauseServiceNotConfigured)},
			want: Chain{
				Stage: StageQueued, Step: StepSummary, QueueReason: QueueServiceNotConfigured, Action: ActionOpenConnections,
				CanRedoSummary: true,
			},
		},
		{
			name: "summary service unavailable", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), caused("a", sum, job.StatusRetryWait, job.CauseServiceUnavailable),
				in("h", hw, job.StatusQueued)},
			want: Chain{
				Stage: StageQueued, Step: StepSummary, QueueReason: QueueServiceUnavailable, JobError: "hub: hub unavailable",
			},
		},
		{
			name: "summary retries without a cause", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), caused("a", sum, job.StatusRetryWait, job.CauseNone)},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueRetry, JobError: "hub: hub unavailable"},
		},
		{
			name: "summary failed while the service was unavailable", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), caused("a", sum, job.StatusFailed, job.CauseServiceUnavailable)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed,
				JobError: "attempts exhausted: hub: hub unavailable", Action: ActionOpenConnections, Retry: WorkSummary,
			},
		},
		{
			name: "homework waits for the quota", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), in("h", hw, job.StatusWaitingForQuota)},
			want: Chain{
				Stage: StageQueued, Step: StepSummary, QueueReason: QueueQuota, Action: ActionOpenConnections, CanRedoSummary: true,
			},
		},
		{
			name: "summary failed", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), in("a", sum, job.StatusFailed), in("h", hw, job.StatusRunning)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, JobError: "bad answer",
				Action: ActionRetry, Retry: WorkSummary,
			},
		},
		{
			name: "homework failed", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), in("h", hw, job.StatusFailed)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorHomeworkFailed, JobError: "bad answer",
				Action: ActionRetry, Retry: WorkHomework, CanRedoSummary: true,
			},
		},
		{
			name: "summary ready", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw)},
			want: Chain{Stage: StageDone, CanRedoSummary: true},
		},
		{
			name: "post-processing without a transcription job", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{in("a", sum, job.StatusWaitingForNetwork), in("h", hw, job.StatusQueued)},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueNetwork},
		},
		{
			name: "a newer transcription after the summary", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusRunning) },
			},
			want: Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: 42},
		},
		{
			name: "a newer transcription completed, its post-processing not enqueued yet", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), in("a", sum, job.StatusFailed), in("h", hw, job.StatusFailed),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusCompleted) },
			},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine},
		},
		{
			name: "a new summary and the homework of the older transcript", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), in("h", hw, job.StatusFailed),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "a2", sum, 2*time.Hour, job.StatusCompleted) },
			},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine, CanRedoSummary: true},
		},
		{
			name: "other job types do not count", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), in("d", "homework_draft", job.StatusFailed)},
			want: Chain{Stage: StageDone, CanRedoSummary: true},
		},
		// TASK-086: a retry for an invalid answer, the connections page only for the LLM call.
		{
			name: "summary answer invalid", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), failed("a", sum, 0, msgInvalid), done("h", hw)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, JobError: msgInvalid,
				Action: ActionRetry, Retry: WorkSummary,
			},
		},
		{
			name: "homework answer cut off", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), failed("h", hw, 0, msgTruncated)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorHomeworkFailed, JobError: msgTruncated,
				Action: ActionRetry, Retry: WorkHomework, CanRedoSummary: true,
			},
		},
		{
			name: "summary LLM call failed", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), failed("a", sum, 0, msgLLM), done("h", hw)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, JobError: msgLLM,
				Action: ActionOpenConnections, Retry: WorkSummary,
			},
		},
		{
			name: "homework LLM call failed for good", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), failed("h", hw, 0, "attempts exhausted: "+msgLLM)},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorHomeworkFailed, JobError: "attempts exhausted: " + msgLLM,
				Action: ActionOpenConnections, Retry: WorkHomework, CanRedoSummary: true,
			},
		},
		{
			name: "summary cancelled", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{done("s", stt), in("a", sum, job.StatusCancelled), done("h", hw)},
			want: Chain{Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, Action: ActionRetry, Retry: WorkSummary},
		},
		{
			name: "a retry runs", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), failed("a", sum, 0, msgInvalid), done("h", hw),
				func(t *testing.T) *job.Job { return jobIn(t, "a2", sum, time.Hour, job.StatusRunning) },
			},
			want: Chain{Stage: StageSummarizing, Step: StepSummary},
		},
		{
			name: "a redo waits", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw),
				func(t *testing.T) *job.Job { return jobIn(t, "a2", sum, time.Hour, job.StatusQueued) },
			},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine},
		},
		{
			name: "a redo failed, the summary before stays", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), failed("a2", sum, time.Hour, msgInvalid),
			},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, JobError: msgInvalid,
				Action: ActionRetry, Retry: WorkSummary, CanRedoSummary: true,
			},
		},
		{
			name: "the summary of an older transcript cannot be redone", rec: recording.StatusCompleted,
			jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusCompleted) },
				failed("a2", sum, 2*time.Hour, msgInvalid),
			},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorSummaryFailed, JobError: msgInvalid,
				Action: ActionRetry, Retry: WorkSummary,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := make([]*job.Job, 0, len(tt.jobs))
			for _, mk := range tt.jobs {
				jobs = append(jobs, mk(t))
			}
			if got := chainOf(recordingIn(t, tt.rec), jobs); got != tt.want {
				t.Fatalf("chain = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}

// Every recording status is mapped (a new status must be placed in the chain on purpose).
func TestChainCoversEveryRecordingStatus(t *testing.T) {
	for _, s := range recording.Statuses() {
		if got := chainOf(recordingIn(t, s), nil); got.Stage == "" {
			t.Errorf("%s has no stage", s)
		}
	}
}

// TASK-082: a silent recording is not transcribed: its row says so and offers the sound check, whatever its jobs.
func TestChainOfSilentRecording(t *testing.T) {
	r := recordingIn(t, recording.StatusFinalizing)
	track, err := r.TrackRef("wav")
	must(t, err)
	must(t, r.CompleteTrack(t0.Add(2*time.Hour), track, values.AssembledTrack{Duration: time.Hour, Seconds: 3600}))

	want := Chain{Stage: StageFailed, ErrorCode: ErrorSilentRecording, Action: ActionCheckSound}
	for _, jobs := range [][]*job.Job{
		nil,
		{jobIn(t, "stt", job.Type("stt"), time.Hour, job.StatusFailed)},
		{jobIn(t, "stt", job.Type("stt"), time.Hour, job.StatusRunning)},
	} {
		if got := chainOf(r, jobs); got != want {
			t.Fatalf("chain %+v, want %+v", got, want)
		}
	}
}
