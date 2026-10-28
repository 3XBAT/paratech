package activity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
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
	hidden     []job.Type

	chain                     map[shared.RecordingID][]*job.Job
	badChain                  []shared.JobID
	chainErr                  error
	byRecording, byTranscript []job.Type

	gets   []shared.JobID
	getErr error
}

// fakeLessons returns the lessons by id; a missing one is shared.ErrNotFound, an id in bad fails.
type fakeLessons struct {
	titles map[shared.LessonID]string
	bad    map[shared.LessonID]error
	gets   int
}

func (f *fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	f.gets++
	if err := f.bad[id]; err != nil {
		return nil, err
	}
	title, ok := f.titles[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: title, Start: t0, End: t0.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
	}}, t0)
}

// ListForRecordings returns the chain jobs of the asked recordings, as the store does.
func (f *fakeJobs) ListForRecordings(_ context.Context, recs []shared.RecordingID, byRecording, byTranscript []job.Type) (
	map[shared.RecordingID][]*job.Job, []shared.JobID, error,
) {
	f.byRecording, f.byTranscript = byRecording, byTranscript
	out := map[shared.RecordingID][]*job.Job{}
	for _, r := range recs {
		if js, ok := f.chain[r]; ok {
			out[r] = js
		}
	}
	return out, f.badChain, f.chainErr
}

// ListActivity leaves out the hidden types as the store does.
func (f *fakeJobs) ListActivity(_ context.Context, limit int, hidden []job.Type) ([]*job.Job, []shared.JobID, error) {
	f.limit, f.hidden = limit, hidden
	var out []*job.Job
	for _, j := range f.list {
		if !slices.Contains(hidden, j.Type()) {
			out = append(out, j)
		}
	}
	return out, f.unreadable, f.err
}

// Get finds a job of any type in the list or the chains, as the store does.
func (f *fakeJobs) Get(_ context.Context, id shared.JobID) (*job.Job, error) {
	f.gets = append(f.gets, id)
	if f.getErr != nil {
		return nil, f.getErr
	}
	all := slices.Clone(f.list)
	for _, js := range f.chain {
		all = append(all, js...)
	}
	for _, j := range all {
		if j.ID() == id {
			return j, nil
		}
	}
	return nil, shared.ErrNotFound
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

// newJob builds a job of a type outside the recording chain: since TASK-076 the chain jobs (stt, summarize,
// extract_homework) are shown inside their recording rows, not in the jobs list.
func newJob(t *testing.T, id string) *job.Job {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID(id), Type: "homework_draft", Key: "homework_draft:" + id, MaxAttempts: 3}, t0)
	must(t, err)
	return j
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(nil, &fakeJobs{}, &fakeLessons{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no recordings: err = %v", err)
	}
	if _, err := New(&fakeRecordings{}, nil, &fakeLessons{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no jobs: err = %v", err)
	}
	if _, err := New(&fakeRecordings{}, &fakeJobs{}, nil, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("no lessons: err = %v", err)
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
	s, err := New(repo, &fakeJobs{}, &fakeLessons{titles: map[shared.LessonID]string{"l1": "История"}}, nil)
	must(t, err)

	got, err := s.ListRecordings(context.Background())
	must(t, err)
	if repo.limit != ListLimit {
		t.Errorf("limit = %d, want %d", repo.limit, ListLimit)
	}
	want := []Recording{
		{
			ID: "r-recording", LessonID: "l1", LessonTitle: "История", Status: recording.StatusRecording,
			Origin: recording.OriginCaptured, StartedAt: t0.Add(time.Second), Stoppable: true,
			Chain: Chain{Stage: StageRecording},
		},
		{
			ID: "r-completed", LessonID: "l1", LessonTitle: "История", Status: recording.StatusCompleted,
			Origin: recording.OriginCaptured, StartedAt: t0, StoppedAt: t0.Add(time.Hour), Duration: 59 * time.Minute,
			HasDuration: true, Chain: Chain{Stage: StageQueued, Step: StepTranscription, QueueReason: QueueInLine},
		},
		{
			ID: "r-failed", LessonID: "l1", LessonTitle: "История", Status: recording.StatusFailed,
			Origin: recording.OriginCaptured, StartedAt: t0, Problem: "device lost", Chain: Chain{Stage: StageFailed, ErrorCode: ErrorRecordingFailed, Action: ActionCheckSound},
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
	s, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil)
	must(t, err)

	got, err := s.ListJobs(context.Background())
	must(t, err)
	if repo.limit != ListLimit {
		t.Errorf("limit = %d, want %d", repo.limit, ListLimit)
	}
	want := []Job{
		{ID: "j-queued", Type: "homework_draft", Status: job.StatusQueued, MaxAttempts: 3, CreatedAt: t0, UpdatedAt: t0, NextRunAt: t0},
		{
			ID: "j-power", Type: "homework_draft", Status: job.StatusWaitingForPower, Waiting: true, MaxAttempts: 3,
			CreatedAt: t0, UpdatedAt: t0.Add(time.Minute), NextRunAt: t0, RunNowOffered: true,
		},
		{
			ID: "j-network", Type: "homework_draft", Status: job.StatusWaitingForNetwork, Waiting: true, MaxAttempts: 3, Percent: 40,
			CreatedAt: t0, UpdatedAt: t0.Add(3 * time.Minute), NextRunAt: t0,
		},
		{
			ID: "j-failed", Type: "homework_draft", Status: job.StatusFailed, Attempts: 1, MaxAttempts: 3, LastError: "bad input",
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
	s, err := New(&fakeRecordings{}, &fakeJobs{}, &fakeLessons{}, nil)
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
	s, err := New(&fakeRecordings{err: boom}, &fakeJobs{err: boom}, &fakeLessons{}, nil)
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
	s, err := New(recs, jobs, &fakeLessons{}, slog.New(slog.NewTextHandler(&logs, nil)))
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
	s, err := New(&fakeRecordings{unreadable: []shared.RecordingID{"r"}}, &fakeJobs{unreadable: []shared.JobID{"j"}}, &fakeLessons{}, nil)
	must(t, err)
	if _, err := s.ListRecordings(context.Background()); err != nil {
		t.Error(err)
	}
	if _, err := s.ListJobs(context.Background()); err != nil {
		t.Error(err)
	}
}

// The hidden service types reach the store, so the store leaves them out before its bound; jobs of the user stay.
// Since TASK-076 the chain types (stt, summarize, extract_homework and, since TASK-090, lecture_text) are always left
// out too: their recording rows show them.
func TestListJobsLeavesOutHiddenTypes(t *testing.T) {
	typed := func(id string, typ job.Type) *job.Job {
		t.Helper()
		j, err := job.New(job.Spec{ID: shared.JobID(id), Type: typ, Key: string(typ) + ":" + id, MaxAttempts: 3}, t0)
		must(t, err)
		return j
	}
	all := []*job.Job{
		typed("sync", "calendar_sync"), typed("stt", "stt"), typed("sum", "summarize"), typed("hw", "extract_homework"),
		typed("lt", "lecture_text"), typed("draft", "homework_draft"), typed("send", "send_email"),
	}
	chain := []job.Type{"stt", "summarize", "extract_homework", "lecture_text"}
	tests := []struct {
		name   string
		hidden []job.Type
		want   []shared.JobID
	}{
		{name: "only the chain hidden", want: []shared.JobID{"sync", "draft", "send"}},
		{name: "calendar sync hidden", hidden: []job.Type{"calendar_sync"}, want: []shared.JobID{"draft", "send"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeJobs{list: all}
			s, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil, tt.hidden...)
			must(t, err)
			got, err := s.ListJobs(context.Background())
			must(t, err)
			if want := append(slices.Clone(tt.hidden), chain...); !slices.Equal(repo.hidden, want) {
				t.Errorf("hidden passed to the store = %v, want %v", repo.hidden, want)
			}
			ids := make([]shared.JobID, 0, len(got))
			for _, j := range got {
				ids = append(ids, j.ID)
			}
			if !slices.Equal(ids, tt.want) {
				t.Errorf("ListJobs = %v, want %v", ids, tt.want)
			}
		})
	}
}

// The caller's slice is copied: changing it later does not change what the screen hides.
func TestNewCopiesHiddenTypes(t *testing.T) {
	hidden := []job.Type{"calendar_sync"}
	repo := &fakeJobs{}
	s, err := New(&fakeRecordings{}, repo, &fakeLessons{}, nil, hidden...)
	must(t, err)
	hidden[0] = "stt"
	_, err = s.ListJobs(context.Background())
	must(t, err)
	if want := []job.Type{"calendar_sync", "stt", "summarize", "extract_homework", "lecture_text"}; !slices.Equal(repo.hidden, want) {
		t.Fatalf("hidden = %v, want %v (the caller's types, then the chain types)", repo.hidden, want)
	}
}

// Popup -> recording -> title (TASK-076): a lesson recording is titled by its lesson; an ad-hoc one and one whose
// lesson is gone have no title (the page shows «Запись <дата, время>»); a lesson that cannot be read leaves only its
// title out and is logged once per change. Each lesson is read once per list.
func TestListRecordingsLessonTitles(t *testing.T) {
	mk := func(id string, l shared.LessonID) *recording.Recording {
		r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), LessonID: l, MaxDuration: 3 * time.Hour}, t0)
		must(t, err)
		return r
	}
	recs := &fakeRecordings{list: []*recording.Recording{
		mk("r-a", "math"), mk("r-b", "math"), mk("r-adhoc", ""), mk("r-gone", "gone"), mk("r-bad", "bad"),
	}}
	lessons := &fakeLessons{
		titles: map[shared.LessonID]string{"math": "Матанализ, лекция"},
		bad:    map[shared.LessonID]error{"bad": errors.New("sql: Scan error on column title")},
	}
	var logs bytes.Buffer
	s, err := New(recs, &fakeJobs{}, lessons, slog.New(slog.NewTextHandler(&logs, nil)))
	must(t, err)

	for range 2 {
		got, err := s.ListRecordings(context.Background())
		must(t, err)
		titles := map[shared.RecordingID]string{}
		for _, r := range got {
			titles[r.ID] = r.LessonTitle
		}
		want := map[shared.RecordingID]string{"r-a": "Матанализ, лекция", "r-b": "Матанализ, лекция", "r-adhoc": "", "r-gone": "", "r-bad": ""}
		if len(got) != len(want) {
			t.Fatalf("rows %+v", got)
		}
		for id, title := range want {
			if titles[id] != title {
				t.Errorf("%s titled %q, want %q", id, titles[id], title)
			}
		}
	}
	if lessons.gets != 6 {
		t.Errorf("lesson reads = %d, want 3 per list (one per lesson)", lessons.gets)
	}
	if n := strings.Count(logs.String(), "lessons that cannot be read"); n != 1 || !strings.Contains(logs.String(), "bad") {
		t.Errorf("lesson warnings = %d, want 1 naming the lesson:\n%s", n, logs.String())
	}
}

// The chain jobs of the listed recordings are asked by the chain types and decide each row's status; a job that
// cannot be read is logged once, and a failure of the query fails the list.
func TestListRecordingsChain(t *testing.T) {
	rec := recordingIn(t, recording.StatusCompleted)
	running := jobIn(t, "stt-1", "stt", 0, job.StatusRunning)
	jobs := &fakeJobs{chain: map[shared.RecordingID][]*job.Job{rec.ID(): {running}}, badChain: []shared.JobID{"stt-bad"}}
	var logs bytes.Buffer
	s, err := New(&fakeRecordings{list: []*recording.Recording{rec}}, jobs, &fakeLessons{}, slog.New(slog.NewTextHandler(&logs, nil)))
	must(t, err)

	for range 2 {
		got, err := s.ListRecordings(context.Background())
		must(t, err)
		if want := (Chain{Stage: StageTranscribing, Step: StepTranscription, Percent: 42}); len(got) != 1 || got[0].Chain != want {
			t.Fatalf("rows %+v, want one with %+v", got, want)
		}
	}
	if !slices.Equal(jobs.byRecording, []job.Type{"stt"}) ||
		!slices.Equal(jobs.byTranscript, []job.Type{"summarize", "extract_homework", "lecture_text"}) {
		t.Errorf("asked types %v / %v", jobs.byRecording, jobs.byTranscript)
	}
	if n := strings.Count(logs.String(), "jobs that cannot be read"); n != 1 || !strings.Contains(logs.String(), "stt-bad") {
		t.Errorf("job warnings = %d:\n%s", n, logs.String())
	}

	boom := errors.New("disk")
	jobs.chainErr = boom
	if _, err := s.ListRecordings(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("ListRecordings err = %v, want %v", err, boom)
	}
}
