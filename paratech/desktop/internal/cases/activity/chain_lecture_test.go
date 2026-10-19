package activity

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
)

// TASK-090: the lecture text comes after the summary and the homework: «Текст лекции…» with its parts while it is
// made, «Повторить» when it failed, and done without a job (a transcript made before the lecture text existed).
func TestChainOfLectureText(t *testing.T) {
	const (
		stt = job.Type("stt")
		sum = job.Type("summarize")
		hw  = job.Type("extract_homework")
		lt  = job.Type("lecture_text")
	)
	done := func(id string, typ job.Type) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobIn(t, id, typ, 0, job.StatusCompleted) }
	}
	in := func(id string, typ job.Type, s job.Status) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobIn(t, id, typ, 0, s) }
	}
	parts := func(id string, s job.Status, percent int, checkpoint string) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job {
			t.Helper()
			j := jobIn(t, id, lt, 0, job.StatusRunning)
			p, err := job.NewProgress(percent, checkpoint)
			must(t, err)
			at := t0.Add(3 * time.Second)
			must(t, j.ReportProgress(p, at))
			if s == job.StatusRetryWait {
				must(t, j.Retry(at, at.Add(time.Minute), "hub timeout"))
			}
			return j
		}
	}
	tests := []struct {
		name string
		jobs []func(t *testing.T) *job.Job
		want Chain
	}{
		{
			name: "no lecture text job", jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw)},
			want: Chain{Stage: StageDone, CanRedoSummary: true},
		},
		{
			name: "made", jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), done("l", lt)},
			want: Chain{Stage: StageDone, CanRedoSummary: true, LectureTextReady: true},
		},
		{
			name: "a new run keeps the text made before", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), done("l", lt),
				func(t *testing.T) *job.Job { return jobIn(t, "l2", lt, time.Hour, job.StatusRunning) },
			},
			want: Chain{Stage: StageLectureText, Step: StepLectureText, Percent: 42, CanRedoSummary: true, LectureTextReady: true},
		},
		{
			name: "a failed new run keeps the text made before", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), done("l", lt),
				failed("l2", lt, time.Hour, msgInvalid),
			},
			want: Chain{
				Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorLectureTextFailed, JobError: msgInvalid,
				Action: ActionRetry, Retry: WorkLectureText, CanRedoSummary: true, LectureTextReady: true,
			},
		},
		{
			name: "the text of an older transcript is not ready", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), done("l", lt),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "a2", sum, 2*time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "h2", hw, 2*time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "l2", lt, 2*time.Hour, job.StatusQueued) },
			},
			want: Chain{Stage: StageQueued, Step: StepLectureText, QueueReason: QueueInLine, CanRedoSummary: true},
		},
		{
			name: "queued", jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), in("l", lt, job.StatusQueued)},
			want: Chain{Stage: StageQueued, Step: StepLectureText, QueueReason: QueueInLine, CanRedoSummary: true},
		},
		{
			name: "runs with parts",
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), parts("l", job.StatusRunning, 25, "3/12")},
			want: Chain{Stage: StageLectureText, Step: StepLectureText, Percent: 25, PartsDone: 3, Parts: 12, CanRedoSummary: true},
		},
		{
			name: "runs before its first report",
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), parts("l", job.StatusRunning, 0, "")},
			want: Chain{Stage: StageLectureText, Step: StepLectureText, CanRedoSummary: true},
		},
		{
			name: "retries with the parts it has",
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), parts("l", job.StatusRetryWait, 50, "6/12")},
			want: Chain{
				Stage: StageQueued, Step: StepLectureText, QueueReason: QueueRetry, JobError: "hub timeout", PartsDone: 6,
				Parts: 12, CanRedoSummary: true,
			},
		},
		{
			name: "answer invalid",
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, msgInvalid)},
			want: Chain{
				Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorLectureTextFailed, JobError: msgInvalid,
				Action: ActionRetry, Retry: WorkLectureText, CanRedoSummary: true,
			},
		},
		{
			name: "LLM call failed",
			jobs: []func(t *testing.T) *job.Job{done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, msgLLM)},
			want: Chain{
				Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorLectureTextFailed, JobError: msgLLM,
				Action: ActionOpenConnections, Retry: WorkLectureText, CanRedoSummary: true,
			},
		},
		{
			name: "the summary comes first", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), in("a", sum, job.StatusRunning), done("h", hw), failed("l", lt, 0, msgInvalid),
			},
			want: Chain{Stage: StageSummarizing, Step: StepSummary},
		},
		{
			name: "a failed homework search comes first", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), in("h", hw, job.StatusFailed), parts("l", job.StatusRunning, 25, "3/12"),
			},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorHomeworkFailed, JobError: "bad answer",
				Action: ActionRetry, Retry: WorkHomework, CanRedoSummary: true,
			},
		},
		{
			name: "the lecture text of an older transcript does not count", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, msgInvalid),
				func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "a2", sum, 2*time.Hour, job.StatusCompleted) },
				func(t *testing.T) *job.Job { return jobIn(t, "h2", hw, 2*time.Hour, job.StatusCompleted) },
			},
			want: Chain{Stage: StageDone, CanRedoSummary: true},
		},
		{
			name: "with only the lecture text job the summary waits first", jobs: []func(t *testing.T) *job.Job{
				in("l", lt, job.StatusQueued),
			},
			want: Chain{Stage: StageQueued, Step: StepSummary, QueueReason: QueueInLine},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := make([]*job.Job, 0, len(tt.jobs))
			for _, mk := range tt.jobs {
				jobs = append(jobs, mk(t))
			}
			if got := chainOf(recordingIn(t, recording.StatusCompleted), jobs); got != tt.want {
				t.Fatalf("chain = %+v\nwant    %+v", got, tt.want)
			}
		})
	}
}
