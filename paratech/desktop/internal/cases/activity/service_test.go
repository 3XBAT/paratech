package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

type fakeRecordings struct {
	list  []*recording.Recording
	err   error
	limit int
}

func (f *fakeRecordings) ListRecent(_ context.Context, limit int) ([]*recording.Recording, error) {
	f.limit = limit
	return f.list, f.err
}

type fakeJobs struct {
	list  []*job.Job
	err   error
	limit int
}

func (f *fakeJobs) ListRecent(_ context.Context, limit int) ([]*job.Job, error) {
	f.limit = limit
	return f.list, f.err
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newRecording(t *testing.T, id string) *recording.Recording {
	t.Helper()
	r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), LessonID: "l1", MaxDuration: 3 * time.Hour}, t0)
	must(t, err)
	return r
}

func newJob(t *testing.T, id string) *job.Job {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID(id), Type: "stt", Key: "stt:" + id, MaxAttempts: 3}, t0)
	must(t, err)
	return j
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(nil, &fakeJobs{}); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no recordings: err = %v", err)
	}
	if _, err := New(&fakeRecordings{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no jobs: err = %v", err)
	}
}

func TestListRecordings(t *testing.T) {
	recordingNow := newRecording(t, "r-recording")
	must(t, recordingNow.Begin(t0.Add(time.Second)))

	completed := newRecording(t, "r-completed")
	must(t, completed.Begin(t0))
	must(t, completed.Stop(t0.Add(time.Hour)))
	must(t, completed.Finalize(t0.Add(time.Hour)))
	track, err := completed.TrackRef("wav")
	must(t, err)
	must(t, completed.Complete(t0.Add(time.Hour+time.Second), track, 59*time.Minute))

	failed := newRecording(t, "r-failed")
	must(t, failed.Fail(t0.Add(time.Second), "device lost"))

	repo := &fakeRecordings{list: []*recording.Recording{recordingNow, completed, failed}}
	s, err := New(repo, &fakeJobs{})
	must(t, err)

	got, err := s.ListRecordings(context.Background())
	must(t, err)
	if repo.limit != ListLimit {
		t.Errorf("limit = %d, want %d", repo.limit, ListLimit)
	}
	want := []Recording{
		{ID: "r-recording", LessonID: "l1", Status: recording.StatusRecording, StartedAt: t0.Add(time.Second), Stoppable: true},
		{
			ID: "r-completed", LessonID: "l1", Status: recording.StatusCompleted, StartedAt: t0, StoppedAt: t0.Add(time.Hour),
			Duration: 59 * time.Minute, HasDuration: true,
		},
		{ID: "r-failed", LessonID: "l1", Status: recording.StatusFailed, StartedAt: t0, Problem: "device lost"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListJobs(t *testing.T) {
	queued := newJob(t, "j-queued")

	power := newJob(t, "j-power")
	must(t, power.WaitFor(job.StatusWaitingForPower, t0.Add(time.Minute)))

	network := newJob(t, "j-network")
	must(t, network.Claim(t0.Add(time.Minute)))
	p, err := job.NewProgress(40, "chunk=4")
	must(t, err)
	must(t, network.ReportProgress(p, t0.Add(2*time.Minute)))
	must(t, network.WaitFor(job.StatusWaitingForNetwork, t0.Add(3*time.Minute)))

	failed := newJob(t, "j-failed")
	must(t, failed.Claim(t0.Add(time.Minute)))
	must(t, failed.Fail(t0.Add(2*time.Minute), "bad input"))

	repo := &fakeJobs{list: []*job.Job{queued, power, network, failed}}
	s, err := New(&fakeRecordings{}, repo)
	must(t, err)

	got, err := s.ListJobs(context.Background())
	must(t, err)
	if repo.limit != ListLimit {
		t.Errorf("limit = %d, want %d", repo.limit, ListLimit)
	}
	want := []Job{
		{ID: "j-queued", Type: "stt", Status: job.StatusQueued, MaxAttempts: 3, CreatedAt: t0, UpdatedAt: t0, NextRunAt: t0},
		{
			ID: "j-power", Type: "stt", Status: job.StatusWaitingForPower, Waiting: true, MaxAttempts: 3,
			CreatedAt: t0, UpdatedAt: t0.Add(time.Minute), NextRunAt: t0, RunNowOffered: true,
		},
		{
			ID: "j-network", Type: "stt", Status: job.StatusWaitingForNetwork, Waiting: true, MaxAttempts: 3, Percent: 40,
			CreatedAt: t0, UpdatedAt: t0.Add(3 * time.Minute), NextRunAt: t0,
		},
		{
			ID: "j-failed", Type: "stt", Status: job.StatusFailed, Attempts: 1, MaxAttempts: 3, LastError: "bad input",
			CreatedAt: t0, UpdatedAt: t0.Add(2 * time.Minute), NextRunAt: t0,
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListsAreEmptyNotNil(t *testing.T) {
	s, err := New(&fakeRecordings{}, &fakeJobs{})
	must(t, err)
	recs, err := s.ListRecordings(context.Background())
	if err != nil || recs == nil {
		t.Errorf("ListRecordings = %v, %v; want an empty slice", recs, err)
	}
	jobs, err := s.ListJobs(context.Background())
	if err != nil || jobs == nil {
		t.Errorf("ListJobs = %v, %v; want an empty slice", jobs, err)
	}
}

func TestListErrorsAreWrapped(t *testing.T) {
	boom := errors.New("disk")
	s, err := New(&fakeRecordings{err: boom}, &fakeJobs{err: boom})
	must(t, err)
	if _, err := s.ListRecordings(context.Background()); !errors.Is(err, boom) {
		t.Errorf("ListRecordings err = %v", err)
	}
	if _, err := s.ListJobs(context.Background()); !errors.Is(err, boom) {
		t.Errorf("ListJobs err = %v", err)
	}
}
