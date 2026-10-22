package activity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

func typedJob(t *testing.T, id string, typ job.Type) *job.Job {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID(id), Type: typ, Key: string(typ) + ":" + id, MaxAttempts: 3}, t0)
	must(t, err)
	return j
}

// The agent asks about the jobs it started, which are the chain and hidden types ListJobs leaves out (TASK-117).
func TestJobStatesSeesEveryType(t *testing.T) {
	summary := typedJob(t, "j-summary", values.SummarizeJobType)
	must(t, summary.Claim(t0.Add(time.Minute)))
	p, err := job.NewProgress(40, "chunk=4")
	must(t, err)
	must(t, summary.ReportProgress(p, t0.Add(2*time.Minute)))
	lecture := typedJob(t, "j-lecture", values.LectureTextJobType)
	stt := typedJob(t, "j-stt", transcription.STTJobType)
	calendar := typedJob(t, "j-calendar", "calendar_sync")

	repo := &fakeJobs{
		list:  []*job.Job{calendar},
		chain: map[shared.RecordingID][]*job.Job{"r1": {stt, summary, lecture}},
	}
	s, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil, "calendar_sync")
	must(t, err)

	got, err := s.JobStates(context.Background(), []shared.JobID{"j-summary", "j-lecture", "j-missing", "j-stt", "j-calendar", "j-summary"})
	must(t, err)
	want := []JobState{
		{ID: "j-summary", Type: values.SummarizeJobType, Status: job.StatusRunning, Percent: 40},
		{ID: "j-lecture", Type: values.LectureTextJobType, Status: job.StatusQueued},
		{ID: "j-stt", Type: transcription.STTJobType, Status: job.StatusQueued},
		{ID: "j-calendar", Type: "calendar_sync", Status: job.StatusQueued},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("states = %+v, want %+v", got, want)
	}
	if len(repo.gets) != 5 {
		t.Errorf("gets = %v, want each id once", repo.gets)
	}
}

func TestJobStatesFailure(t *testing.T) {
	boom := errors.New("disk I/O error")
	s, err := New(&fakeRecordings{}, &fakeJobs{getErr: boom}, &fakeLessons{}, nil)
	must(t, err)
	if _, err := s.JobStates(context.Background(), []shared.JobID{"j1"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}
