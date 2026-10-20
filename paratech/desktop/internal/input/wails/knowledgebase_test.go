package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/knowledgebase"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/shared"
)

// fakeKnowledge is the knowledge base use case: fixed answers and what it was asked.
type fakeKnowledge struct {
	err       error
	exportErr error
	subjects  []kb.Subject
	lessons   []kb.Lesson
	result    knowledgebase.SearchResult
	name      string
	key       string
	query     string
	path      string
	set       [3]string
}

func (f *fakeKnowledge) Subjects(context.Context) ([]kb.Subject, error) { return f.subjects, f.err }

func (f *fakeKnowledge) Lessons(_ context.Context, key string) ([]kb.Lesson, error) {
	f.key = key
	return f.lessons, f.err
}

func (f *fakeKnowledge) Lesson(_ context.Context, id shared.RecordingID) (kb.Lesson, error) {
	if f.err != nil || len(f.lessons) == 0 {
		return kb.Lesson{}, errors.Join(f.err, knowledgebase.ErrRecordingNotFound)
	}
	return f.lessons[0], nil
}

func (f *fakeKnowledge) Search(_ context.Context, query string) (knowledgebase.SearchResult, error) {
	f.query = query
	return f.result, f.err
}

func (f *fakeKnowledge) SubjectFileName(_ context.Context, key string) (string, error) {
	f.key = key
	return f.name, f.err
}

func (f *fakeKnowledge) ExportSubject(_ context.Context, key, path string) error {
	f.key, f.path = key, path
	return f.exportErr
}

func (f *fakeKnowledge) SetLessonInfo(_ context.Context, id shared.RecordingID, subject, kind string) (kb.Lesson, error) {
	f.set = [3]string{string(id), subject, kind}
	if f.err != nil {
		return kb.Lesson{}, f.err
	}
	return f.lessons[0], nil
}

func newKnowledgeService(f *fakeKnowledge, pick func(context.Context, string) (string, error)) (*KnowledgeBaseService, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	return NewKnowledgeBaseService(f, knowledgePickerFunc(pick), slog.New(slog.NewTextHandler(logs, nil))), logs
}

var kbStart = time.Date(2026, 10, 19, 7, 0, 0, 0, time.UTC)

func kbLesson() kb.Lesson {
	return kb.Lesson{
		Recording: "r1", StartedAt: kbStart, Duration: 90 * time.Minute, Title: "Механика", Subject: "Физика",
		Kind: "lecture", SubjectEdited: true, Ready: kb.Readiness{Transcript: "t1", Summary: true},
	}
}

// The page reads these names (frontend/src/bindings/knowledgeBase.ts).
func TestKnowledgeJSON(t *testing.T) {
	ctx := context.Background()
	f := &fakeKnowledge{
		subjects: []kb.Subject{
			{Key: "физика", Name: "Физика", Lessons: 2, Latest: kbStart},
			{Name: kb.NoSubjectName, Lessons: 1, Latest: kbStart, NoSubject: true},
		},
		lessons: []kb.Lesson{kbLesson()},
		result: knowledgebase.SearchResult{Truncated: true, Hits: []knowledgebase.SearchHit{{
			Hit: kb.Hit{Recording: "r1", Section: kb.SectionTranscript, Index: 7, StartMs: 65_000,
				Snippet: kb.Snippet{Before: "…про ", Match: "кванты", After: " <b>"}},
			Lesson: kbLesson(),
		}, {
			Hit: kb.Hit{Recording: "r1", Section: kb.SectionTitle, Snippet: kb.Snippet{Match: "Механика"}}, Lesson: kbLesson(),
		}}},
	}
	s, _ := newKnowledgeService(f, nil)

	subjects, err := s.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(subjects)
	if want := `[{"key":"физика","name":"Физика","lessons":2,"latest":"2026-10-19T07:00:00Z","noSubject":false},` +
		`{"key":"","name":"Без предмета","lessons":1,"latest":"2026-10-19T07:00:00Z","noSubject":true}]`; string(raw) != want {
		t.Fatalf("Subjects JSON = %s\nwant %s", raw, want)
	}

	lessons, err := s.Lessons(ctx, "физика")
	if err != nil || f.key != "физика" {
		t.Fatalf("Lessons = %v, key %q", err, f.key)
	}
	raw, _ = json.Marshal(lessons)
	lessonJSON := `{"recordingId":"r1","startedAt":"2026-10-19T07:00:00Z","durationSeconds":5400,"title":"Механика",` +
		`"subject":"Физика","subjectKey":"физика","kind":"lecture","subjectEdited":true,"kindEdited":false,` +
		`"transcriptReady":true,"summaryReady":true,"lectureTextReady":false}`
	if string(raw) != "["+lessonJSON+"]" {
		t.Fatalf("Lessons JSON = %s\nwant [%s]", raw, lessonJSON)
	}

	res, err := s.Search(ctx, "кванты")
	if err != nil || f.query != "кванты" {
		t.Fatalf("Search = %v, query %q", err, f.query)
	}
	raw, _ = json.Marshal(res)
	want := `{"hits":[{"recordingId":"r1","section":"transcript","index":7,"startMs":65000,"time":"01:05",` +
		`"before":"…про ","match":"кванты","after":" \u003cb\u003e","lesson":` + lessonJSON + `},` +
		`{"recordingId":"r1","section":"title","index":0,"startMs":0,"time":"","before":"","match":"Механика","after":"",` +
		`"lesson":` + lessonJSON + `}],"truncated":true}`
	if string(raw) != want {
		t.Fatalf("Search JSON = %s\nwant %s", raw, want)
	}

	f.lessons, f.subjects = nil, nil
	if got, err := s.Lessons(ctx, "x"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no lessons = %#v, %v; want an empty list", got, err)
	}
	if got, err := s.Subjects(ctx); err != nil || got == nil {
		t.Fatalf("no subjects = %#v, %v; want an empty list", got, err)
	}
	f.result = knowledgebase.SearchResult{}
	if got, err := s.Search(ctx, "кванты"); err != nil || got.Hits == nil {
		t.Fatalf("no hits = %#v, %v; want an empty list", got, err)
	}
}

func TestKnowledgeSetLessonInfo(t *testing.T) {
	ctx := context.Background()
	f := &fakeKnowledge{lessons: []kb.Lesson{kbLesson()}}
	s, _ := newKnowledgeService(f, nil)
	got, err := s.SetLessonInfo(ctx, "r1", "Физика", "lecture")
	if err != nil || got.RecordingID != "r1" || f.set != [3]string{"r1", "Физика", "lecture"} {
		t.Fatalf("SetLessonInfo = %+v, %v, asked %v", got, err, f.set)
	}
	if _, err := s.SetLessonInfo(ctx, "a/b", "", ""); !errors.Is(err, ErrKnowledgeInvalidID) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := s.Lesson(ctx, "r1"); err != nil {
		t.Fatalf("Lesson: %v", err)
	}
	if _, err := s.Lesson(ctx, "../x"); !errors.Is(err, ErrKnowledgeInvalidID) {
		t.Fatalf("Lesson bad id: %v", err)
	}
}

func TestKnowledgeErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		err  error
		want error
	}{
		{knowledgebase.ErrRecordingNotFound, ErrKnowledgeNotFound},
		{knowledgebase.ErrSubjectNotFound, ErrKnowledgeSubjectNotFound},
		{knowledgebase.ErrInvalidQuery, ErrKnowledgeQuery},
		{knowledgebase.ErrInvalidLessonInfo, ErrKnowledgeLessonInfo},
		{knowledgebase.ErrNotFinished, ErrKnowledgeNotFinished},
		{knowledgebase.ErrBusy, ErrKnowledgeBusy},
		{knowledgebase.ErrInvalidRequest, ErrKnowledgeInvalidID},
		{errors.Join(knowledgebase.ErrUnavailable, errors.New(`open C:\Users\student\db: locked`)), ErrKnowledgeUnavailable},
	}
	for _, tc := range tests {
		f := &fakeKnowledge{err: tc.err, lessons: []kb.Lesson{kbLesson()}}
		s, logs := newKnowledgeService(f, nil)
		calls := map[string]func() error{
			"Subjects":      func() error { _, err := s.Subjects(ctx); return err },
			"Lessons":       func() error { _, err := s.Lessons(ctx, "x"); return err },
			"Search":        func() error { _, err := s.Search(ctx, "xx"); return err },
			"ExportSubject": func() error { _, err := s.ExportSubject(ctx, "x"); return err },
			"SetLessonInfo": func() error { _, err := s.SetLessonInfo(ctx, "r1", "", ""); return err },
		}
		for name, call := range calls {
			if err := call(); !errors.Is(err, tc.want) || strings.Contains(err.Error(), "student") {
				t.Errorf("%s with %v = %v, want %v", name, tc.err, err, tc.want)
			}
		}
		if tc.want == ErrKnowledgeUnavailable && !strings.Contains(logs.String(), "locked") {
			t.Errorf("the cause is not logged: %s", logs)
		}
	}
}

func TestKnowledgeExportSubject(t *testing.T) {
	ctx := context.Background()
	var proposed string
	pick := func(path string, err error) func(context.Context, string) (string, error) {
		return func(_ context.Context, name string) (string, error) {
			proposed = name
			return path, err
		}
	}

	f := &fakeKnowledge{name: "Физика.md"}
	s, _ := newKnowledgeService(f, pick(`C:\Users\student\Физика.md`, nil))
	got, err := s.ExportSubject(ctx, "физика")
	if err != nil || got != (SummarySaveResult{Status: transcriptSaved, Message: msgSubjectSaved, Name: "Физика.md"}) {
		t.Fatalf("ExportSubject = %+v, %v", got, err)
	}
	if proposed != "Физика.md" || f.key != "физика" || f.path != `C:\Users\student\Физика.md` {
		t.Fatalf("proposed %q, exported %q to %q", proposed, f.key, f.path)
	}

	f.path = ""
	s, _ = newKnowledgeService(f, pick("", nil))
	if got, err := s.ExportSubject(ctx, "физика"); err != nil || got.Status != transcriptCancelled || f.path != "" {
		t.Fatalf("cancelled = %+v, %v, wrote %q", got, err, f.path)
	}

	s, logs := newKnowledgeService(f, pick("", errors.New("dialog broke")))
	if got, err := s.ExportSubject(ctx, "физика"); err != nil || got.Status != transcriptFailed || !strings.Contains(logs.String(), "dialog broke") {
		t.Fatalf("dialog error = %+v, %v", got, err)
	}

	f.exportErr = errors.Join(knowledgebase.ErrUnavailable, errors.New("disk full"))
	s, _ = newKnowledgeService(f, pick(`C:\x.md`, nil))
	if got, err := s.ExportSubject(ctx, "физика"); err != nil || got != (SummarySaveResult{Status: transcriptFailed, Message: msgSubjectFailed}) {
		t.Fatalf("write error = %+v, %v", got, err)
	}
	f.exportErr = knowledgebase.ErrSubjectNotFound
	if _, err := s.ExportSubject(ctx, "физика"); !errors.Is(err, ErrKnowledgeSubjectNotFound) {
		t.Fatalf("subject gone = %v", err)
	}
}

// TestFrontendKnowsKnowledgeErrors keeps the error texts the knowledge base page shows as they are equal to the
// binding's.
func TestFrontendKnowsKnowledgeErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/knowledgeBase.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []error{
		ErrKnowledgeInvalidID, ErrKnowledgeNotFound, ErrKnowledgeSubjectNotFound, ErrKnowledgeQuery, ErrKnowledgeLessonInfo,
		ErrKnowledgeNotFinished, ErrKnowledgeBusy, ErrKnowledgeUnavailable,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/knowledgeBase.ts does not list the error %q", e)
		}
	}
}
