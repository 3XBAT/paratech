package summaryview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
	"paratech/desktop/testenv"
)

var t0 = time.Date(2026, 10, 19, 9, 5, 0, 0, time.UTC)

// msk is the location of the clock: titles, dates and file names use it.
var msk = time.FixedZone("MSK", 3*3600)

const summaryText = "Реки и их дельты.\n\nДельта — устье реки."

var errDisk = errors.New(`open C:\Users\student\db: locked`)

type fakeRecordings map[shared.RecordingID]*recording.Recording

func (f fakeRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if r, ok := f[id]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("%w: recording %s", shared.ErrNotFound, id)
}

type fakeLessons struct {
	rows map[shared.LessonID]*lesson.Lesson
	err  error
}

func (f *fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if f.err != nil {
		return nil, f.err
	}
	if l, ok := f.rows[id]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("%w: lesson %s", shared.ErrNotFound, id)
}

type fakeTranscripts struct {
	rows map[shared.RecordingID]*transcript.Transcript
	err  error
}

func (f *fakeTranscripts) LatestByRecording(_ context.Context, rec shared.RecordingID) (*transcript.Transcript, error) {
	if f.err != nil {
		return nil, f.err
	}
	if t, ok := f.rows[rec]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("%w: transcript of %s", shared.ErrNotFound, rec)
}

type fakeSummaries struct {
	rows map[summary.ID]*summary.Summary
	err  error
}

func (f *fakeSummaries) Get(_ context.Context, id summary.ID) (*summary.Summary, error) {
	if f.err != nil {
		return nil, f.err
	}
	if s, ok := f.rows[id]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("%w: summary %s", shared.ErrNotFound, id)
}

type fakeAssignments struct {
	rows map[shared.TranscriptID][]*assignment.Assignment
	err  error
}

func (f *fakeAssignments) ListByTranscript(_ context.Context, t shared.TranscriptID) ([]*assignment.Assignment, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[t], nil
}

type fakeJobs struct {
	byPrefix map[string]*job.Job
	prefixes []string
	err      error
}

func (f *fakeJobs) LatestByKeyPrefix(_ context.Context, typ job.Type, prefix string) (*job.Job, error) {
	f.prefixes = append(f.prefixes, string(typ)+" "+prefix)
	if f.err != nil {
		return nil, f.err
	}
	if j, ok := f.byPrefix[prefix]; ok && j.Type() == typ {
		return j, nil
	}
	return nil, fmt.Errorf("%w: job", shared.ErrNotFound)
}

type fakeWriter struct {
	path    string
	content string
	err     error
}

func (f *fakeWriter) WriteFile(_ context.Context, path string, content []byte) error {
	if f.err != nil {
		return f.err
	}
	f.path, f.content = path, string(content)
	return nil
}

type env struct {
	svc         *Service
	recordings  fakeRecordings
	lessons     *fakeLessons
	transcripts *fakeTranscripts
	summaries   *fakeSummaries
	assignments *fakeAssignments
	jobs        *fakeJobs
	writer      *fakeWriter
	logs        *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		recordings:  fakeRecordings{},
		lessons:     &fakeLessons{rows: map[shared.LessonID]*lesson.Lesson{}},
		transcripts: &fakeTranscripts{rows: map[shared.RecordingID]*transcript.Transcript{}},
		summaries:   &fakeSummaries{rows: map[summary.ID]*summary.Summary{}},
		assignments: &fakeAssignments{rows: map[shared.TranscriptID][]*assignment.Assignment{}},
		jobs:        &fakeJobs{byPrefix: map[string]*job.Job{}},
		writer:      &fakeWriter{},
		logs:        &bytes.Buffer{},
	}
	svc, err := New(Deps{
		Recordings: e.recordings, Lessons: e.lessons, Transcripts: e.transcripts, Summaries: e.summaries,
		Assignments: e.assignments, Jobs: e.jobs, Writer: e.writer, Clock: testenv.NewFakeClock(t0.In(msk)),
		Log: slog.New(slog.NewTextHandler(e.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// addRecording stores a completed recording of 85 minutes, optionally of a lesson titled title.
func (e *env) addRecording(t *testing.T, id shared.RecordingID, title string) {
	t.Helper()
	var lid shared.LessonID
	if title != "" {
		lid = shared.LessonID("lesson-" + string(id))
		l, err := lesson.New(lesson.Spec{ID: lid, Content: lesson.Content{
			Title: title, Start: t0, End: t0.Add(time.Hour), TimeZone: "Europe/Moscow",
		}}, t0)
		if err != nil {
			t.Fatal(err)
		}
		e.lessons.rows[lid] = l
	}
	r, err := recording.New(recording.Spec{ID: id, LessonID: lid, MaxDuration: 3 * time.Hour}, t0)
	if err != nil {
		t.Fatal(err)
	}
	end := t0.Add(85 * time.Minute)
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(r.Begin(t0), r.Stop(end), r.Finalize(end), r.Complete(end, track, 85*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e.recordings[id] = r
}

// addTranscript stores the transcript "tr-<rec>" of rec and returns its id.
func (e *env) addTranscript(t *testing.T, rec shared.RecordingID) shared.TranscriptID {
	t.Helper()
	id := shared.TranscriptID("tr-" + string(rec))
	ref, err := transcript.DocumentRef(id)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transcript.Restore(transcript.State{Spec: transcript.Spec{
		ID: id, RecordingID: rec, ModelID: "m1", ModelName: "base", ModelVersion: "1", Text: ref,
	}, Version: 1, CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	e.transcripts.rows[rec] = tr
	return id
}

func (e *env) addSummary(t *testing.T, tr shared.TranscriptID, text string, points ...string) {
	t.Helper()
	c, err := summary.NewContent(text, points)
	if err != nil {
		t.Fatal(err)
	}
	s, err := summary.New(summary.Spec{Transcript: tr, PromptVersion: 1, Content: c}, t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e.summaries.rows[s.ID()] = s
}

func (e *env) addAssignment(t *testing.T, tr shared.TranscriptID, title, description string, due time.Time) shared.AssignmentID {
	t.Helper()
	d, err := assignment.NewDetails(title, description, due)
	if err != nil {
		t.Fatal(err)
	}
	index := len(e.assignments.rows[tr])
	a, err := assignment.NewExtracted(assignment.Spec{
		ID: shared.AssignmentID(fmt.Sprintf("a%d", index+1)), Origin: assignment.Origin{Transcript: tr, Index: index},
		PromptVersion: 1, Details: d,
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.assignments.rows[tr] = append(e.assignments.rows[tr], a)
	return a.ID()
}

// addJob stores the post-processing job of typ for tr in status st, with cause for a waiting job.
func (e *env) addJob(t *testing.T, typ string, tr shared.TranscriptID, st job.Status, cause job.Cause) {
	t.Helper()
	j, err := job.New(job.Spec{ID: shared.JobID(typ + "-" + string(tr)), Type: job.Type(typ), Key: typ + ":" + string(tr) + ":v1", MaxAttempts: 3}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if st != job.StatusQueued {
		if err := j.Claim(t0); err != nil {
			t.Fatal(err)
		}
	}
	switch st {
	case job.StatusFailed:
		err = j.Fail(t0, "homeworkflow: the LLM answer does not match the schema")
	case job.StatusCancelled:
		err = j.Cancel(t0)
	case job.StatusCompleted:
		err = j.Complete(t0)
	case job.StatusRetryWait:
		err = j.RetryCause(t0, t0.Add(time.Minute), "homeworkflow: the LLM call failed", cause)
	case job.StatusWaitingForNetwork, job.StatusWaitingForQuota, job.StatusWaitingForPower:
		err = j.WaitForCause(st, cause, t0)
	}
	if err != nil {
		t.Fatal(err)
	}
	e.jobs.byPrefix[typ+":"+string(tr)+":"] = j
}

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(empty) = %v", err)
	}
}

func TestViewOfAReadySummary(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "География")
	tr := e.addTranscript(t, "r1")
	e.addSummary(t, tr, summaryText, "Реки текут в море", "Дельта")
	due := time.Date(2026, 10, 21, 20, 59, 0, 0, time.UTC)
	e.addAssignment(t, tr, "Прочитать главу 4", "Стр. 40-52", due)
	e.addAssignment(t, tr, "Нарисовать карту", "", time.Time{})
	e.addJob(t, "summarize", tr, job.StatusCompleted, job.CauseNone)
	e.addJob(t, "extract_homework", tr, job.StatusCompleted, job.CauseNone)

	v, err := e.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Recording != "r1" || v.Lesson != "lesson-r1" || v.Title != "География" || !v.StartedAt.Equal(t0) ||
		!v.HasDuration || v.Duration != 85*time.Minute || !v.Transcribed {
		t.Fatalf("head = %+v", v)
	}
	if v.Summary.Step != (Step{State: StateDone}) || !v.Summary.Ready || v.Summary.Content.Text() != summaryText ||
		!reflect.DeepEqual(v.Summary.Content.KeyPoints(), []string{"Реки текут в море", "Дельта"}) ||
		!v.Summary.UpdatedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("summary = %+v", v.Summary)
	}
	want := []Assignment{
		{ID: "a1", Title: "Прочитать главу 4", Description: "Стр. 40-52", Due: due},
		{ID: "a2", Title: "Нарисовать карту"},
	}
	if v.Homework.Step != (Step{State: StateDone}) || !reflect.DeepEqual(v.Homework.Assignments, want) {
		t.Fatalf("homework = %+v", v.Homework)
	}
	wantPrefixes := []string{"summarize summarize:tr-r1:", "extract_homework extract_homework:tr-r1:"}
	if !reflect.DeepEqual(e.jobs.prefixes, wantPrefixes) {
		t.Fatalf("job prefixes = %v", e.jobs.prefixes)
	}
	if strings.Contains(e.logs.String(), "Реки") || strings.Contains(e.logs.String(), "главу") {
		t.Fatalf("model text reached the log: %s", e.logs.String())
	}
}

func TestViewStates(t *testing.T) {
	type jobSpec struct {
		status job.Status
		cause  job.Cause
	}
	tests := []struct {
		name        string
		summary     *jobSpec // nil: no job
		homework    *jobSpec
		stored      bool // a summary and an assignment are stored
		wantSummary Step
		wantReady   bool
		wantHW      Step
	}{
		{
			name:        "jobs not enqueued yet",
			wantSummary: Step{State: StateWaiting, Wait: WaitInLine}, wantHW: Step{State: StateWaiting, Wait: WaitInLine},
		},
		{
			name: "results kept without jobs", stored: true,
			wantSummary: Step{State: StateDone}, wantReady: true, wantHW: Step{State: StateDone},
		},
		{
			name:    "queued and running",
			summary: &jobSpec{status: job.StatusQueued}, homework: &jobSpec{status: job.StatusRunning},
			wantSummary: Step{State: StateWaiting, Wait: WaitInLine}, wantHW: Step{State: StateRunning},
		},
		{
			name:    "no network and no quota",
			summary: &jobSpec{status: job.StatusWaitingForNetwork}, homework: &jobSpec{status: job.StatusWaitingForQuota},
			wantSummary: Step{State: StateWaiting, Wait: WaitNetwork}, wantHW: Step{State: StateWaiting, Wait: WaitQuota},
		},
		{
			name:        "service not configured comes before the network status",
			summary:     &jobSpec{status: job.StatusWaitingForNetwork, cause: job.CauseServiceNotConfigured},
			homework:    &jobSpec{status: job.StatusWaitingForPower},
			wantSummary: Step{State: StateWaiting, Wait: WaitServiceNotConfigured}, wantHW: Step{State: StateWaiting, Wait: WaitPower},
		},
		{
			name:    "retries",
			summary: &jobSpec{status: job.StatusRetryWait}, homework: &jobSpec{status: job.StatusRetryWait, cause: job.CauseServiceUnavailable},
			wantSummary: Step{State: StateWaiting, Wait: WaitRetry, JobError: "homeworkflow: the LLM call failed"},
			wantHW:      Step{State: StateWaiting, Wait: WaitServiceUnavailable, JobError: "homeworkflow: the LLM call failed"},
		},
		{
			name:    "failed and cancelled",
			summary: &jobSpec{status: job.StatusFailed}, homework: &jobSpec{status: job.StatusCancelled},
			wantSummary: Step{State: StateFailed, JobError: "homeworkflow: the LLM answer does not match the schema"},
			wantHW:      Step{State: StateFailed},
		},
		{
			name:    "a failed rerun keeps the summary made before",
			summary: &jobSpec{status: job.StatusFailed}, homework: &jobSpec{status: job.StatusRunning}, stored: true,
			wantSummary: Step{State: StateFailed, JobError: "homeworkflow: the LLM answer does not match the schema"},
			wantReady:   true, wantHW: Step{State: StateRunning},
		},
		{
			name:    "completed without a stored summary",
			summary: &jobSpec{status: job.StatusCompleted}, homework: &jobSpec{status: job.StatusCompleted},
			wantSummary: Step{State: StateNone}, wantHW: Step{State: StateDone},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addRecording(t, "r1", "")
			tr := e.addTranscript(t, "r1")
			if tc.stored {
				e.addSummary(t, tr, summaryText)
				e.addAssignment(t, tr, "Задание", "", time.Time{})
			}
			if tc.summary != nil {
				e.addJob(t, "summarize", tr, tc.summary.status, tc.summary.cause)
			}
			if tc.homework != nil {
				e.addJob(t, "extract_homework", tr, tc.homework.status, tc.homework.cause)
			}

			v, err := e.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			if v.Summary.Step != tc.wantSummary || v.Summary.Ready != tc.wantReady {
				t.Errorf("summary = %+v, ready %v; want %+v, ready %v", v.Summary.Step, v.Summary.Ready, tc.wantSummary, tc.wantReady)
			}
			if v.Homework.Step != tc.wantHW {
				t.Errorf("homework = %+v, want %+v", v.Homework.Step, tc.wantHW)
			}
		})
	}
}

func TestViewBeforeTranscription(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")

	v, err := e.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Transcribed || v.Summary.Step != (Step{State: StateNone}) || v.Summary.Ready ||
		v.Homework.Step != (Step{State: StateNone}) || v.Homework.Assignments == nil || len(v.Homework.Assignments) != 0 {
		t.Fatalf("view = %+v", v)
	}
	if v.Title != "Запись 19.10.2026, 12:05" {
		t.Fatalf("ad-hoc title = %q", v.Title)
	}
	if len(e.jobs.prefixes) != 0 {
		t.Fatalf("jobs looked up without a transcript: %v", e.jobs.prefixes)
	}
}

func TestViewErrors(t *testing.T) {
	tests := []struct {
		name string
		id   shared.RecordingID
		edit func(e *env)
		want error
	}{
		{name: "invalid id", id: "a/b", want: ErrInvalidRequest},
		{name: "empty id", id: "", want: ErrInvalidRequest},
		{name: "missing recording", id: "nope", want: ErrRecordingNotFound},
		{name: "transcripts unreadable", id: "r1", edit: func(e *env) { e.transcripts.err = errDisk }, want: ErrUnavailable},
		{name: "summaries unreadable", id: "r1", edit: func(e *env) { e.summaries.err = errDisk }, want: ErrUnavailable},
		{name: "assignments unreadable", id: "r1", edit: func(e *env) { e.assignments.err = errDisk }, want: ErrUnavailable},
		{name: "jobs unreadable", id: "r1", edit: func(e *env) { e.jobs.err = errDisk }, want: ErrUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addRecording(t, "r1", "")
			e.addTranscript(t, "r1")
			if tc.edit != nil {
				tc.edit(e)
			}
			if _, err := e.svc.View(context.Background(), tc.id); !errors.Is(err, tc.want) {
				t.Fatalf("View = %v, want %v", err, tc.want)
			}
		})
	}
}

// A lesson that cannot be read only costs the title; the cause is logged.
func TestLessonFailureFallsBackToTheDate(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "География")
	e.lessons.err = errDisk

	v, err := e.svc.View(context.Background(), "r1")
	if err != nil || v.Title != "Запись 19.10.2026, 12:05" {
		t.Fatalf("View = %q, %v", v.Title, err)
	}
	if !strings.Contains(e.logs.String(), opLesson) {
		t.Fatalf("the cause is not logged: %s", e.logs.String())
	}
}

func TestText(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "География")
	tr := e.addTranscript(t, "r1")
	e.addSummary(t, tr, summaryText, "Дельта")
	e.addAssignment(t, tr, "Прочитать главу 4", "", time.Date(2026, 10, 21, 20, 59, 0, 0, time.UTC))

	got, err := e.svc.Text(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	want := "География\n19.10.2026, 12:05 · 1 ч 25 мин\n\n" + summaryText + "\n" +
		"\nКлючевые моменты:\n- Дельта\n" +
		"\nДомашние задания:\n- Прочитать главу 4 — до 21.10.2026, 23:59\n"
	if got != want {
		t.Fatalf("Text =\n%q\nwant\n%q", got, want)
	}
}

func TestTextWithoutSummary(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	if _, err := e.svc.Text(context.Background(), "r1"); !errors.Is(err, ErrNoSummary) {
		t.Fatalf("no transcript: %v", err)
	}
	e.addTranscript(t, "r1")
	if _, err := e.svc.Text(context.Background(), "r1"); !errors.Is(err, ErrNoSummary) {
		t.Fatalf("no summary: %v", err)
	}
	if _, err := e.svc.Text(context.Background(), "missing"); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("missing recording: %v", err)
	}
}

func TestSuggestedName(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", `Физика: 9/А`)
	e.addRecording(t, "r2", "")
	tests := []struct {
		id   shared.RecordingID
		f    summaryexport.Format
		want string
	}{
		{"r1", summaryexport.FormatMarkdown, "Конспект Физика_ 9_А 2026-10-19.md"},
		{"r2", summaryexport.FormatText, "Конспект 2026-10-19 12-05.txt"},
	}
	for _, tc := range tests {
		got, err := e.svc.SuggestedName(context.Background(), tc.id, tc.f)
		if err != nil || got != tc.want {
			t.Errorf("SuggestedName(%s) = %q, %v; want %q", tc.id, got, err, tc.want)
		}
	}
	if _, err := e.svc.SuggestedName(context.Background(), "r1", "html"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("unknown format: %v", err)
	}
	if _, err := e.svc.SuggestedName(context.Background(), "nope", summaryexport.FormatText); !errors.Is(err, ErrRecordingNotFound) {
		t.Errorf("missing recording: %v", err)
	}
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	e.addRecording(t, "r1", "")
	tr := e.addTranscript(t, "r1")
	e.addSummary(t, tr, "<b>Жирный</b> текст")

	path := `C:\Users\student\Конспект.md`
	if err := e.svc.Export(context.Background(), "r1", summaryexport.FormatMarkdown, path); err != nil {
		t.Fatal(err)
	}
	want := "# Запись 19.10.2026, 12:05\n\n19\\.10.2026, 12:05 · 1 ч 25 мин\n\n\\<b\\>Жирный\\</b\\> текст\n"
	if e.writer.path != path || e.writer.content != want {
		t.Fatalf("wrote %q to %q, want %q", e.writer.content, e.writer.path, want)
	}

	tests := []struct {
		name string
		f    summaryexport.Format
		path string
		edit func(e *env)
		want error
	}{
		{name: "unknown format", f: "html", path: path, want: ErrInvalidRequest},
		{name: "no path", f: summaryexport.FormatText, want: ErrInvalidRequest},
		{name: "write failed", f: summaryexport.FormatText, path: path, edit: func(e *env) { e.writer.err = errDisk }, want: ErrUnavailable},
		{name: "no summary", f: summaryexport.FormatText, path: path, edit: func(e *env) { delete(e.summaries.rows, summary.IDOf(tr)) }, want: ErrNoSummary},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.edit != nil {
				tc.edit(e)
			}
			if err := e.svc.Export(context.Background(), "r1", tc.f, tc.path); !errors.Is(err, tc.want) {
				t.Fatalf("Export = %v, want %v", err, tc.want)
			}
		})
	}
}
