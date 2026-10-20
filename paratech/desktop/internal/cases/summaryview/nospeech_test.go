package summaryview

import (
	"context"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	tvalues "paratech/desktop/internal/domain/transcription/values"
)

// failedWith stores a job of typ with id that failed permanently with msg, found by prefix.
func (e *env) failedWith(t *testing.T, id shared.JobID, typ job.Type, prefix, msg string) {
	t.Helper()
	j, err := job.New(job.Spec{ID: id, Type: typ, Key: prefix + "k", MaxAttempts: 3}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Claim(t0); err != nil {
		t.Fatal(err)
	}
	if err := j.Fail(t0, "permanent failure: "+msg); err != nil {
		t.Fatal(err)
	}
	e.jobs.byPrefix[prefix] = j
}

// TASK-110: the summary page of a recording without speech says so in every step and offers nothing to retry: when
// its STT job found only marks, and when the lecture text of a transcript stored before found nothing to process.
func TestViewOfARecordingWithoutSpeech(t *testing.T) {
	none := Step{State: StateNoSpeech}
	tests := []struct {
		name  string
		setup func(e *env, tr shared.TranscriptID)
		want  bool
	}{
		{name: "the transcription found no speech", want: true, setup: func(e *env, tr shared.TranscriptID) {
			e.failedWith(t, shared.JobID(tr), sttJobType, "stt:r1:", tvalues.NoSpeechMessage)
		}},
		{name: "the lecture text found nothing to process", want: true, setup: func(e *env, tr shared.TranscriptID) {
			e.addJob(t, "summarize", tr, job.StatusCompleted, job.CauseNone)
			e.addJob(t, "extract_homework", tr, job.StatusCompleted, job.CauseNone)
			e.failedWith(t, "lt-1", lectureJobType, "lecture_text:"+string(tr)+":", values.NothingToProcessMessage)
		}},
		{name: "the STT job of another transcript", setup: func(e *env, tr shared.TranscriptID) {
			e.failedWith(t, "stt-other", sttJobType, "stt:r1:", tvalues.NoSpeechMessage)
		}},
		{name: "another failure", setup: func(e *env, tr shared.TranscriptID) {
			e.addJob(t, "summarize", tr, job.StatusFailed, job.CauseNone)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addRecording(t, "r1", "География")
			tc.setup(e, e.addTranscript(t, "r1"))
			v, err := e.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			if v.NoSpeech != tc.want {
				t.Fatalf("NoSpeech = %v, want %v", v.NoSpeech, tc.want)
			}
			if !tc.want {
				return
			}
			if v.Summary.Step != none || v.Homework.Step != none || v.LectureText.Step != none || v.LectureText.CanStart {
				t.Fatalf("view = %+v", v)
			}
		})
	}
}
