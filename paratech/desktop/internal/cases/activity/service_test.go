package activity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

type fakeRecordings struct {
	list       []*recording.Recording
	unreadable []shared.RecordingID
	err        error
	limit      int
}

func (f *fakeRecordings) ListActivity(_ context.Context, limit int) ([]*recording.Recording, []shared.RecordingID, error) {
	f.limit = limit
	return f.list, f.unreadable, f.err
}

type fakeJobs struct {
	list       []*job.Job
	unreadable []shared.JobID
	err        error
	limit      int
}

func (f *fakeJobs) ListActivity(_ context.Context, limit int) ([]*job.Job, []shared.JobID, error) {
	f.limit = limit
	return f.list, f.unreadable, f.err
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
	if _, err := New(nil, &fakeJobs{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no recordings: err = %v", err)
	}
	if _, err := New(&fakeRecordings{}, nil, nil); !errors.Is(err, ErrInvalidConfig) {
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
	s, err := New(repo, &fakeJobs{}, nil)
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
	s, err := New(&fakeRecordings{}, repo, nil)
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
	s, err := New(&fakeRecordings{}, &fakeJobs{}, nil)
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
	s, err := New(&fakeRecordings{err: boom}, &fakeJobs{err: boom}, nil)
	must(t, err)
	if _, err := s.ListRecordings(context.Background()); !errors.Is(err, boom) {
		t.Errorf("ListRecordings err = %v", err)
	}
	if _, err := s.ListJobs(context.Background()); !errors.Is(err, boom) {
		t.Errorf("ListJobs err = %v", err)
	}
}

// Unreadable rows do not fail the lists; their ids are logged when they change, not on every poll of the screen.
func TestUnreadableRowsAreLoggedWhenTheyChange(t *testing.T) {
	var logs bytes.Buffer
	recs := &fakeRecordings{list: []*recording.Recording{newRecording(t, "r-ok")}, unreadable: []shared.RecordingID{"r-bad"}}
	jobs := &fakeJobs{list: []*job.Job{newJob(t, "j-ok")}, unreadable: []shared.JobID{"j-bad"}}
	s, err := New(recs, jobs, slog.New(slog.NewTextHandler(&logs, nil)))
	must(t, err)
	ctx := context.Background()

	steps := []struct {
		name        string
		badRecs     []shared.RecordingID
		badJobs     []shared.JobID
		wantRecLogs int
		wantJobLogs int
	}{
		{name: "first poll reports", badRecs: []shared.RecordingID{"r-bad"}, badJobs: []shared.JobID{"j-bad"}, wantRecLogs: 1, wantJobLogs: 1},
		{name: "same ids stay quiet", badRecs: []shared.RecordingID{"r-bad"}, badJobs: []shared.JobID{"j-bad"}, wantRecLogs: 1, wantJobLogs: 1},
		{name: "new ids report", badRecs: []shared.RecordingID{"r-bad", "r-bad2"}, badJobs: []shared.JobID{"j-bad2"}, wantRecLogs: 2, wantJobLogs: 2},
		{name: "healed is quiet", wantRecLogs: 2, wantJobLogs: 2},
		{name: "broken again reports", badRecs: []shared.RecordingID{"r-bad"}, wantRecLogs: 3, wantJobLogs: 2},
	}
	for _, st := range steps {
		recs.unreadable, jobs.unreadable = st.badRecs, st.badJobs
		gotRecs, err := s.ListRecordings(ctx)
		if err != nil || len(gotRecs) != 1 || gotRecs[0].ID != "r-ok" {
			t.Fatalf("%s: ListRecordings = %+v, %v; want the readable row", st.name, gotRecs, err)
		}
		gotJobs, err := s.ListJobs(ctx)
		if err != nil || len(gotJobs) != 1 || gotJobs[0].ID != "j-ok" {
			t.Fatalf("%s: ListJobs = %+v, %v; want the readable row", st.name, gotJobs, err)
		}
		if n := strings.Count(logs.String(), "unreadable recordings"); n != st.wantRecLogs {
			t.Errorf("%s: recording warnings = %d, want %d:\n%s", st.name, n, st.wantRecLogs, logs.String())
		}
		if n := strings.Count(logs.String(), "unreadable jobs"); n != st.wantJobLogs {
			t.Errorf("%s: job warnings = %d, want %d:\n%s", st.name, n, st.wantJobLogs, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "r-bad2") || !strings.Contains(logs.String(), "j-bad2") {
		t.Errorf("the ids are not logged:\n%s", logs.String())
	}
}

// Without a logger the unreadable rows are still left out quietly.
func TestNilLoggerDiscards(t *testing.T) {
	s, err := New(&fakeRecordings{unreadable: []shared.RecordingID{"r"}}, &fakeJobs{unreadable: []shared.JobID{"j"}}, nil)
	must(t, err)
	if _, err := s.ListRecordings(context.Background()); err != nil {
		t.Error(err)
	}
	if _, err := s.ListJobs(context.Background()); err != nil {
		t.Error(err)
	}
}
