package activity

import (
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/recording/recording"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// TASK-110: a recording whose transcript holds no speech shows «В записи нет речи» with the sound check and no
// «Повторить»: when the STT job found only marks, and when a post-processing job of a transcript stored before that
// check found nothing to process (the owner's case: 14 of 15 lecture text parts, then the merge failed).
func TestChainOfNoSpeech(t *testing.T) {
	const (
		stt = job.Type("stt")
		sum = job.Type("summarize")
		hw  = job.Type("extract_homework")
		lt  = job.Type("lecture_text")
	)
	// The runner stores a permanent failure with its marker.
	permanent := func(msg string) string { return backoff.ErrPermanent.Error() + ": " + msg }
	done := func(id string, typ job.Type) func(t *testing.T) *job.Job {
		return func(t *testing.T) *job.Job { return jobIn(t, id, typ, 0, job.StatusCompleted) }
	}
	noSpeechSTT := failed("s", stt, 0, permanent(transcription.NoSpeechMessage))
	empty := permanent(values.NothingToProcessMessage)
	// The fixed errors of homeworkflow (ErrLectureTextEmpty, ErrIncompleteContext); cases do not import each other.
	emptyLecture := permanent("post-processing: the lecture text came out empty")
	incomplete := permanent("post-processing: the lesson context is incomplete")
	tests := []struct {
		name string
		jobs []func(t *testing.T) *job.Job
		want Chain
	}{
		{
			name: "the transcription found no speech", jobs: []func(t *testing.T) *job.Job{noSpeechSTT},
			want: Chain{Stage: StageFailed, Step: StepTranscription, ErrorCode: ErrorNoSpeech, Action: ActionCheckSound},
		},
		{
			name: "the lecture text found nothing to process", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, empty),
			},
			want: Chain{Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorNoSpeech, Action: ActionCheckSound},
		},
		{
			name: "the summary found nothing to process", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), failed("a", sum, 0, empty), done("h", hw), done("l", lt),
			},
			want: Chain{Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorNoSpeech, Action: ActionCheckSound},
		},
		{
			name: "no speech wins over another failure", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), failed("a", sum, 0, msgInvalid), done("h", hw), failed("l", lt, 0, empty),
			},
			want: Chain{Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorNoSpeech, Action: ActionCheckSound},
		},
		{
			name: "a retry after nothing to process fails the same way", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, empty), failed("l2", lt, time.Hour, empty),
			},
			want: Chain{Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorNoSpeech, Action: ActionCheckSound},
		},
		{
			name: "a newer transcription replaces the one without speech", jobs: []func(t *testing.T) *job.Job{
				noSpeechSTT, func(t *testing.T) *job.Job { return jobIn(t, "s2", stt, time.Hour, job.StatusRunning) },
			},
			want: Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: 42},
		},
		{
			// A transcript with speech whose model answers held no block: the normal failure with «Повторить».
			name: "an empty lecture text is not no speech", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), done("h", hw), failed("l", lt, 0, emptyLecture),
			},
			want: Chain{
				Stage: StageFailed, Step: StepLectureText, ErrorCode: ErrorLectureTextFailed, JobError: emptyLecture,
				Action: ActionRetry, Retry: WorkLectureText, CanRedoSummary: true,
			},
		},
		{
			name: "a missing lesson start is not no speech", jobs: []func(t *testing.T) *job.Job{
				done("s", stt), done("a", sum), failed("h", hw, 0, incomplete), done("l", lt),
			},
			want: Chain{
				Stage: StageFailed, Step: StepSummary, ErrorCode: ErrorHomeworkFailed, JobError: incomplete,
				Action: ActionRetry, Retry: WorkHomework, CanRedoSummary: true, LectureTextReady: true,
			},
		},
		{
			name: "another failure of the transcription is not no speech",
			jobs: []func(t *testing.T) *job.Job{failed("s", stt, 0, "stt job: transcription failed")},
			want: Chain{
				Stage: StageFailed, Step: StepTranscription, ErrorCode: ErrorTranscriptionFailed,
				JobError: "stt job: transcription failed", Action: ActionOpenModels,
			},
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
