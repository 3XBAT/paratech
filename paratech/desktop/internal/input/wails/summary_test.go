package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/lecturetext"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/coursework/summary"
	"paratech/desktop/internal/domain/shared"
)

// fakeSummary is SummaryUseCase; err, when set, is returned by every method.
type fakeSummary struct {
	err      error
	view     summaryview.View
	text     string
	name     string
	exported struct {
		id   shared.RecordingID
		f    summaryexport.Format
		path string
	}
	exportErr error
	calls     []string

	lectureTimes bool // the times argument of the last lecture text call
}

func (f *fakeSummary) LectureText(_ context.Context, id shared.RecordingID, times bool) (string, error) {
	f.calls = append(f.calls, "LectureText "+string(id))
	f.lectureTimes = times
	return f.text, f.err
}

func (f *fakeSummary) LectureSuggestedName(_ context.Context, _ shared.RecordingID, fm summaryexport.Format) (string, error) {
	f.calls = append(f.calls, "LectureSuggestedName "+string(fm))
	return f.name, f.err
}

func (f *fakeSummary) ExportLectureText(_ context.Context, id shared.RecordingID, fm summaryexport.Format, times bool,
	path string) error {
	f.calls = append(f.calls, "ExportLectureText")
	f.exported.id, f.exported.f, f.exported.path, f.lectureTimes = id, fm, path, times
	if f.exportErr != nil {
		return f.exportErr
	}
	return f.err
}

func (f *fakeSummary) View(_ context.Context, id shared.RecordingID) (summaryview.View, error) {
	f.calls = append(f.calls, "View "+string(id))
	return f.view, f.err
}

func (f *fakeSummary) Text(_ context.Context, id shared.RecordingID) (string, error) {
	f.calls = append(f.calls, "Text "+string(id))
	return f.text, f.err
}

func (f *fakeSummary) SuggestedName(_ context.Context, _ shared.RecordingID, fm summaryexport.Format) (string, error) {
	f.calls = append(f.calls, "SuggestedName "+string(fm))
	return f.name, f.err
}

func (f *fakeSummary) Export(_ context.Context, id shared.RecordingID, fm summaryexport.Format, path string) error {
	f.calls = append(f.calls, "Export")
	f.exported.id, f.exported.f, f.exported.path = id, fm, path
	if f.exportErr != nil {
		return f.exportErr
	}
	return f.err
}

type summaryFixture struct {
	svc      *SummaryService
	uc       *fakeSummary
	clip     *fakeClipboard
	picked   []string
	pickPath string
	pickErr  error
	logs     *bytes.Buffer
}

func newSummaryFixture() *summaryFixture {
	f := &summaryFixture{uc: &fakeSummary{}, clip: &fakeClipboard{}, logs: &bytes.Buffer{}}
	picker := summaryPickerFunc(func(_ context.Context, name, format string) (string, error) {
		f.picked = append(f.picked, name+" "+format)
		return f.pickPath, f.pickErr
	})
	f.svc = NewSummaryService(f.uc, picker, f.clip, slog.New(slog.NewTextHandler(f.logs, nil)))
	return f
}

// everySummaryCall calls each bound method with the recording id.
var everySummaryCall = []struct {
	name string
	call func(s *SummaryService, id string) error
}{
	{"View", func(s *SummaryService, id string) error { _, err := s.View(context.Background(), id); return err }},
	{"CopyAll", func(s *SummaryService, id string) error { return s.CopyAll(context.Background(), id) }},
	{"SaveAs", func(s *SummaryService, id string) error {
		_, err := s.SaveAs(context.Background(), id, "md")
		return err
	}},
}

func TestEverySummaryMethodIsCovered(t *testing.T) {
	if n := len(everySummaryCall); n != reflect.TypeFor[*SummaryService]().NumMethod() {
		t.Fatalf("everySummaryCall has %d calls, SummaryService has %d methods", n, reflect.TypeFor[*SummaryService]().NumMethod())
	}
}

// Ids are checked before the use case is called; use case errors become fixed texts without their causes.
func TestSummaryErrors(t *testing.T) {
	for _, c := range everySummaryCall {
		t.Run(c.name, func(t *testing.T) {
			f := newSummaryFixture()
			for _, bad := range []string{"", "a/b", `..\x`} {
				if err := c.call(f.svc, bad); !errors.Is(err, ErrSummaryInvalidID) {
					t.Errorf("id %q: err = %v", bad, err)
				}
			}
			if len(f.uc.calls) != 0 {
				t.Fatalf("the use case was called with an invalid id: %v", f.uc.calls)
			}

			for _, tc := range []struct{ err, want error }{
				{summaryview.ErrRecordingNotFound, ErrSummaryNotFound},
				{summaryview.ErrNoSummary, ErrSummaryEmpty},
				{summaryview.ErrInvalidRequest, ErrSummaryInvalidID},
				{errors.Join(summaryview.ErrUnavailable, errors.New(`open C:\Users\student\x: denied`)), ErrSummaryUnavailable},
			} {
				f.uc.err = tc.err
				err := c.call(f.svc, "r1")
				if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "student") {
					t.Errorf("%v: err = %v, want %v", tc.err, err, tc.want)
				}
			}
			if len(f.picked) != 0 {
				t.Fatalf("the save dialog was shown for a failing use case: %v", f.picked)
			}
		})
	}
}

func TestSummaryView(t *testing.T) {
	started := time.Date(2026, 10, 14, 7, 1, 0, 0, time.FixedZone("MSK", 3*3600))
	content, err := summary.NewContent("Реки.\n\nДельты.", []string{"Реки текут", "Дельта"})
	if err != nil {
		t.Fatal(err)
	}
	f := newSummaryFixture()
	f.uc.view = summaryview.View{
		Recording: "r1", Lesson: "l1", Title: "География", StartedAt: started, Duration: 85*time.Minute + 30*time.Second,
		HasDuration: true, Transcribed: true,
		Summary: summaryview.Summary{
			Step: summaryview.Step{State: summaryview.StateDone}, Ready: true, Content: content, UpdatedAt: started.Add(2 * time.Hour),
		},
		Homework: summaryview.Homework{
			Step: summaryview.Step{State: summaryview.StateDone},
			Assignments: []summaryview.Assignment{
				{ID: "a1", Title: "<b>Глава 4</b>", Description: "Стр. 40", Due: started.Add(48 * time.Hour), UserEdited: true},
				{ID: "a2", Title: "Карта"},
			},
		},
		LectureText: summaryview.LectureText{
			Step: summaryview.Step{State: summaryview.StateFailed, JobError: "post-processing: stopped"}, Ready: true,
			Blocks:    []lecturetext.Block{lectureBlock(t, "<i>Введение</i>", 3_725_400, "Сегодня о реках.", "Дельты.")},
			UpdatedAt: started.Add(3 * time.Hour), PartsDone: 3, Parts: 12,
		},
	}

	got, err := f.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	want := SummaryView{
		RecordingID: "r1", LessonID: "l1", Title: "География", StartedAt: "2026-10-14T04:01:00Z", DurationSeconds: 5130,
		HasDuration: true, Transcribed: true,
		Summary: SummaryContent{
			SummaryStep: SummaryStep{State: "done", Message: msgSummaryDone}, Ready: true, Text: "Реки.\n\nДельты.",
			KeyPoints: []string{"Реки текут", "Дельта"}, UpdatedAt: "2026-10-14T06:01:00Z",
		},
		Homework: SummaryHomework{
			SummaryStep: SummaryStep{State: "done", Message: msgHomeworkDone},
			Assignments: []SummaryAssignment{
				{ID: "a1", Title: "<b>Глава 4</b>", Description: "Стр. 40", Due: "2026-10-16T04:01:00Z", UserEdited: true},
				{ID: "a2", Title: "Карта"},
			},
		},
		LectureText: SummaryLectureText{
			SummaryStep: SummaryStep{State: "failed", Message: msgLectureFailed, JobError: "post-processing: stopped"},
			Ready:       true,
			Blocks: []SummaryLectureBlock{{
				Heading: "<i>Введение</i>", StartSeconds: 3725, Time: "1:02:05", Paragraphs: []string{"Сегодня о реках.", "Дельты."},
			}},
			UpdatedAt: "2026-10-14T07:01:00Z", PartsDone: 3, Parts: 12, RetryWork: "lectureText",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("View =\n%+v\nwant\n%+v", got, want)
	}
	if strings.Contains(f.logs.String(), "Реки") {
		t.Fatal("the summary text reached the log")
	}
}

// Lists are never null in JSON: the page iterates them without checks.
func TestSummaryViewOfNothingYet(t *testing.T) {
	f := newSummaryFixture()
	f.uc.view = summaryview.View{
		Recording: "r1", Summary: summaryview.Summary{Step: summaryview.Step{State: summaryview.StateNone}},
		Homework: summaryview.Homework{Step: summaryview.Step{State: summaryview.StateNone}},
	}
	got, err := f.svc.View(context.Background(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"keyPoints":[]`) || !strings.Contains(string(data), `"assignments":[]`) ||
		got.Summary.Message != msgSummaryNone || got.Homework.Message != msgHomeworkNone || got.DurationSeconds != 0 {
		t.Fatalf("View = %s", data)
	}
}

func TestSummaryStepMessages(t *testing.T) {
	tests := []struct {
		name     string
		step     summaryview.Step
		summary  SummaryStep
		homework SummaryStep
	}{
		{
			name: "in line", step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitInLine},
			summary:  SummaryStep{State: "waiting", WaitReason: "inLine", Message: msgSummaryWaiting},
			homework: SummaryStep{State: "waiting", WaitReason: "inLine", Message: msgHomeworkWaiting},
		},
		{
			name: "network", step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitNetwork},
			summary:  SummaryStep{State: "waiting", WaitReason: "network", Message: msgWaitNetwork},
			homework: SummaryStep{State: "waiting", WaitReason: "network", Message: msgWaitNetwork},
		},
		{
			name: "not configured", step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitServiceNotConfigured},
			summary:  SummaryStep{State: "waiting", WaitReason: "serviceNotConfigured", Message: msgWaitNotConfigured},
			homework: SummaryStep{State: "waiting", WaitReason: "serviceNotConfigured", Message: msgWaitNotConfigured},
		},
		{
			name: "retry", step: summaryview.Step{State: summaryview.StateWaiting, Wait: summaryview.WaitRetry, JobError: "homeworkflow: the LLM call failed"},
			summary:  SummaryStep{State: "waiting", WaitReason: "retry", Message: msgWaitRetry, JobError: "homeworkflow: the LLM call failed"},
			homework: SummaryStep{State: "waiting", WaitReason: "retry", Message: msgWaitRetry, JobError: "homeworkflow: the LLM call failed"},
		},
		{
			name: "running", step: summaryview.Step{State: summaryview.StateRunning},
			summary:  SummaryStep{State: "running", Message: msgSummaryRunning},
			homework: SummaryStep{State: "running", Message: msgHomeworkRunning},
		},
		{
			name: "failed", step: summaryview.Step{State: summaryview.StateFailed, JobError: "x"},
			summary:  SummaryStep{State: "failed", Message: msgSummaryFailedText, JobError: "x"},
			homework: SummaryStep{State: "failed", Message: msgHomeworkFailedText, JobError: "x"},
		},
		{
			name: "done without homework", step: summaryview.Step{State: summaryview.StateDone},
			summary:  SummaryStep{State: "done", Message: msgSummaryDone},
			homework: SummaryStep{State: "done", Message: msgHomeworkDoneNone},
		},
		{
			name: "none after transcription", step: summaryview.Step{State: summaryview.StateNone},
			summary:  SummaryStep{State: "none", Message: msgSummaryMissing},
			homework: SummaryStep{State: "none", Message: msgHomeworkNone},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSummaryFixture()
			f.uc.view = summaryview.View{
				Recording: "r1", Transcribed: true,
				Summary: summaryview.Summary{Step: tc.step}, Homework: summaryview.Homework{Step: tc.step},
			}
			got, err := f.svc.View(context.Background(), "r1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Summary.SummaryStep != tc.summary || got.Homework.SummaryStep != tc.homework {
				t.Fatalf("steps = %+v / %+v, want %+v / %+v", got.Summary.SummaryStep, got.Homework.SummaryStep, tc.summary, tc.homework)
			}
		})
	}
}

func TestSummaryCopyAll(t *testing.T) {
	f := newSummaryFixture()
	f.uc.text = "Конспект: текст"
	if err := f.svc.CopyAll(context.Background(), "r1"); err != nil || f.clip.text != "Конспект: текст" {
		t.Fatalf("CopyAll = %v, clipboard %q", err, f.clip.text)
	}

	f.clip.err = errors.New("clipboard busy")
	if err := f.svc.CopyAll(context.Background(), "r1"); !errors.Is(err, ErrSummaryClipboard) {
		t.Fatalf("clipboard failure: %v", err)
	}
	if strings.Contains(f.logs.String(), "текст") {
		t.Fatal("the summary text reached the log")
	}
}

func TestSummarySaveAs(t *testing.T) {
	f := newSummaryFixture()
	f.uc.name = "Конспект Физика 2026-10-19.md"
	f.pickPath = `C:\Users\student\Документы\Конспект Физика 2026-10-19.md`

	got, err := f.svc.SaveAs(context.Background(), "r1", "md")
	if err != nil {
		t.Fatal(err)
	}
	if got != (SummarySaveResult{Status: "saved", Message: msgSummarySaved, Name: "Конспект Физика 2026-10-19.md"}) {
		t.Fatalf("SaveAs = %+v", got)
	}
	if !reflect.DeepEqual(f.picked, []string{"Конспект Физика 2026-10-19.md md"}) {
		t.Fatalf("picker got %v", f.picked)
	}
	if e := f.uc.exported; e.id != "r1" || e.f != summaryexport.FormatMarkdown || e.path != f.pickPath {
		t.Fatalf("exported %+v", e)
	}
}

func TestSummarySaveAsOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		pickPath  string
		pickErr   error
		exportErr error
		want      SummarySaveResult
		wantErr   error
	}{
		{name: "cancelled", format: "txt", want: SummarySaveResult{Status: "cancelled", Message: msgSummaryCancelled}},
		{name: "dialog failed", format: "txt", pickErr: errors.New("dialog"), want: SummarySaveResult{Status: "failed", Message: msgSummaryFailed}},
		{name: "write failed", format: "txt", pickPath: `C:\student\x.txt`,
			exportErr: errors.Join(summaryview.ErrUnavailable, errors.New("access denied")),
			want:      SummarySaveResult{Status: "failed", Message: msgSummaryFailed}},
		{name: "summary gone meanwhile", format: "txt", pickPath: `C:\student\x.txt`, exportErr: summaryview.ErrNoSummary,
			wantErr: ErrSummaryEmpty},
		{name: "unknown format", format: "html", wantErr: ErrSummaryFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newSummaryFixture()
			f.pickPath, f.pickErr, f.uc.exportErr = tc.pickPath, tc.pickErr, tc.exportErr
			got, err := f.svc.SaveAs(context.Background(), "r1", tc.format)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("SaveAs = %+v, %v; want %+v, %v", got, err, tc.want, tc.wantErr)
			}
			if strings.Contains(f.logs.String(), "student") {
				t.Fatalf("the path reached the log: %s", f.logs.String())
			}
		})
	}
}

func TestSummaryJSONNames(t *testing.T) {
	data, err := json.Marshal(SummaryView{Homework: SummaryHomework{Assignments: []SummaryAssignment{{}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"recordingId", "lessonId", "title", "startedAt", "durationSeconds", "hasDuration", "transcribed", "summary",
		"homework", "state", "waitReason", "message", "jobError", "ready", "text", "keyPoints", "updatedAt",
		"assignments", "id", "description", "due", "userEdited",
	} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("view JSON %s has no %q", data, k)
		}
	}
	data, _ = json.Marshal(SummarySaveResult{})
	for _, k := range []string{"status", "message", "name"} {
		if !strings.Contains(string(data), `"`+k+`"`) {
			t.Errorf("save JSON %s has no %q", data, k)
		}
	}
}

// The page shows the error texts as they are; the frontend wrapper lists each one.
func TestFrontendKnowsSummaryErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/summary.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrSummaryInvalidID, ErrSummaryNotFound, ErrSummaryEmpty, ErrSummaryFormat, ErrSummaryUnavailable, ErrSummaryClipboard,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/summary.ts does not list the error %q", e)
		}
	}
}
