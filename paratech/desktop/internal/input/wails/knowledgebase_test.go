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
	lectures  []kb.Lesson
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

func (f *fakeKnowledge) Lectures(_ context.Context, key string) ([]kb.Lesson, error) {
	f.key = key
	return f.lectures, f.err
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
		Key: kb.RecordingKey("r1"), Recording: "r1", ScheduleLesson: "l1", StartedAt: kbStart, Duration: 90 * time.Minute, Title: "Механика", Subject: "Физика",
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
	if want := `[{"key":"физика","name":"Физика","lessons":2,"latest":"2026-10-19T07:00:00Z","noSubject":false,"subjectId":""},` +
		`{"key":"","name":"Без предмета","lessons":1,"latest":"2026-10-19T07:00:00Z","noSubject":true,"subjectId":""}]`; string(raw) != want {
		t.Fatalf("Subjects JSON = %s\nwant %s", raw, want)
	}

	lessons, err := s.Lessons(ctx, "физика")
	if err != nil || f.key != "физика" {
		t.Fatalf("Lessons = %v, key %q", err, f.key)
	}
	raw, _ = json.Marshal(lessons)
	lessonJSON := `{"key":"recording:r1","noteOnly":false,"undated":false,"anchor":"recording","scheduleLessonId":"l1",` +
		`"date":"","notes":[],"recordingId":"r1","startedAt":"2026-10-19T07:00:00Z","durationSeconds":5400,"title":"Механика",` +
		`"subject":"Физика","subjectKey":"физика","kind":"lecture","subjectEdited":true,"kindEdited":false,` +
		`"transcriptReady":true,"summaryReady":true,"lectureTextReady":false,` +
		`"librarySubjectId":"","librarySubjectState":"none"}`
	if string(raw) != "["+lessonJSON+"]" {
		t.Fatalf("Lessons JSON = %s\nwant [%s]", raw, lessonJSON)
	}

	res, err := s.Search(ctx, "кванты")
	if err != nil || f.query != "кванты" {
		t.Fatalf("Search = %v, query %q", err, f.query)
	}
	raw, _ = json.Marshal(res)
	noNote := `"noteId":"","materialId":"","locatorKind":"","locatorN":0,"label":"",`
	want := `{"hits":[{"recordingId":"r1","section":"transcript","index":7,"startMs":65000,"time":"01:05",` +
		`"before":"…про ","match":"кванты","after":" \u003cb\u003e",` + noNote + `"lesson":` + lessonJSON + `},` +
		`{"recordingId":"r1","section":"title","index":0,"startMs":0,"time":"","before":"","match":"Механика","after":"",` +
		noNote + `"lesson":` + lessonJSON + `}],"truncated":true}`
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

// The page takes the library subject of a lesson from the core (TASK-252): an id only when matched, and the state.
func TestKnowledgeLessonLibrarySubject(t *testing.T) {
	phys := kb.LibrarySubject{ID: "phys", Name: "Физика"}
	tests := []struct {
		name      string
		lesson    func(*kb.Lesson)
		wantID    string
		wantState string
	}{
		{"matched", func(l *kb.Lesson) { l.Library = phys }, "phys", "matched"},
		{"none", func(*kb.Lesson) {}, "", "none"},
		{"ambiguous", func(l *kb.Lesson) { l.Subject, l.LibraryAmbiguous = "Матан", true }, "", "ambiguous"},
		{"no subject", func(l *kb.Lesson) { l.Subject = "" }, "", "no-subject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := kbLesson()
			tt.lesson(&l)
			s, _ := newKnowledgeService(&fakeKnowledge{lessons: []kb.Lesson{l}}, nil)
			got, err := s.Lesson(context.Background(), "r1")
			if err != nil || got.LibrarySubjectID != tt.wantID || got.LibrarySubjectState != tt.wantState {
				t.Fatalf("Lesson = id %q state %q, %v; want %q %q", got.LibrarySubjectID, got.LibrarySubjectState, err, tt.wantID, tt.wantState)
			}
		})
	}
}

// TestFrontendKnowsLibrarySubjectStates keeps the states the binding sends equal to those the page accepts.
func TestFrontendKnowsLibrarySubjectStates(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/knowledgeBase.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []kb.LibraryMatch{kb.LibraryMatched, kb.LibraryNone, kb.LibraryAmbiguous, kb.LibraryNoSubject} {
		if !strings.Contains(string(src), `"`+string(s)+`"`) {
			t.Errorf("frontend/src/bindings/knowledgeBase.ts does not list the library subject state %q", s)
		}
	}
}

var (
	kbZone = time.FixedZone("MSK", 3*60*60) // a note-only lecture starts at local midnight, the day before in UTC
	kbPhys = kb.LibrarySubject{ID: "phys", Name: "Физика"}
)

// noteLecture is a note-only lecture of kbPhys as kb.WithNotes makes it.
func noteLecture(key string, day time.Time, lesson shared.LessonID, note shared.NoteID) kb.Lesson {
	return kb.Lesson{
		Key: key, ScheduleLesson: lesson, StartedAt: day, Title: "Ряды", Subject: "Физика", Kind: "lecture", Library: kbPhys,
		Notes: []kb.Note{{ID: note, Title: "Ряды.docx", Subject: "phys", Parts: []shared.MaterialID{"m1"}, CreatedAt: kbStart}},
	}
}

// TASK-155: Lectures lists the recorded lessons with their notes and the note-only lectures in the core's order; the
// page tells them apart and opens them by ids only.
func TestKnowledgeLectures(t *testing.T) {
	ctx := context.Background()
	recorded := kbLesson()
	recorded.Notes = []kb.Note{{ID: "n0", Title: "Механика", Subject: "phys", CreatedAt: kbStart}}
	day := noteLecture(kb.DayKey("phys", time.Date(2026, 9, 12, 0, 0, 0, 0, kbZone)), time.Date(2026, 9, 12, 0, 0, 0, 0, kbZone), "", "n1")
	lesson := noteLecture(kb.LessonKey("l7"), time.Date(2026, 9, 5, 0, 0, 0, 0, kbZone), "l7", "n2")
	undated := noteLecture(kb.NoteKey("n3"), time.Time{}, "", "n3")
	f := &fakeKnowledge{lectures: []kb.Lesson{recorded, day, lesson, undated}}
	s, _ := newKnowledgeService(f, nil)

	got, err := s.Lectures(ctx, "ID:phys")
	if err != nil || f.key != "ID:phys" || len(got) != 4 {
		t.Fatalf("Lectures = %+v, %v, key %q", got, err, f.key)
	}
	raw, _ := json.Marshal(got[1])
	want := `{"key":"day:phys:2026-09-12","noteOnly":true,"undated":false,"anchor":"day","scheduleLessonId":"",` +
		`"date":"2026-09-12","notes":[{"id":"n1","title":"Ряды.docx","subjectId":"phys","createdAt":"2026-10-19T07:00:00Z"}],` +
		`"recordingId":"","startedAt":"2026-09-11T21:00:00Z","durationSeconds":0,"title":"Ряды","subject":"Физика",` +
		`"subjectKey":"ID:phys","kind":"lecture","subjectEdited":false,"kindEdited":false,"transcriptReady":false,` +
		`"summaryReady":false,"lectureTextReady":false,"librarySubjectId":"phys","librarySubjectState":"matched"}`
	if string(raw) != want {
		t.Fatalf("note-only lecture JSON = %s\nwant %s", raw, want)
	}

	tests := []struct {
		key, anchor, date, startedAt, recording, lesson, note string
		noteOnly, undated                                     bool
	}{
		{"recording:r1", "recording", "", "2026-10-19T07:00:00Z", "r1", "l1", "n0", false, false},
		{"day:phys:2026-09-12", "day", "2026-09-12", "2026-09-11T21:00:00Z", "", "", "n1", true, false},
		{"lesson:l7", "lesson", "2026-09-05", "2026-09-04T21:00:00Z", "", "l7", "n2", true, false},
		{"note:n3", "undated", "", "", "", "", "n3", true, true},
	}
	for i, tt := range tests {
		l := got[i]
		if l.Key != tt.key || l.Anchor != tt.anchor || l.Date != tt.date || l.StartedAt != tt.startedAt ||
			l.RecordingID != tt.recording || l.ScheduleLessonID != tt.lesson || l.NoteOnly != tt.noteOnly ||
			l.Undated != tt.undated || len(l.Notes) != 1 || l.Notes[0].ID != tt.note {
			t.Errorf("lecture %d = %+v, want %+v", i, l, tt)
		}
	}

	f.lectures = nil
	if got, err := s.Lectures(ctx, "x"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no lectures = %#v, %v; want an empty list", got, err)
	}
}

// A subject known only from the student's notes is listed (no recorded lesson, no latest) with its library subject.
func TestKnowledgeSubjectsFromNotes(t *testing.T) {
	f := &fakeKnowledge{subjects: []kb.Subject{
		{Key: "физика", Name: "Физика", Lessons: 1, Latest: kbStart},
		{Key: kb.LibraryKey("chem"), Name: "Химия", SubjectID: "chem"},
	}}
	s, _ := newKnowledgeService(f, nil)
	got, err := s.Subjects(context.Background())
	want := []KnowledgeSubject{
		{Key: "физика", Name: "Физика", Lessons: 1, Latest: "2026-10-19T07:00:00Z"},
		{Key: "ID:chem", Name: "Химия", SubjectID: "chem"},
	}
	if err != nil || len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Subjects = %+v, %v; want %+v", got, err, want)
	}
}

// TASK-155, TASK-295: hits in the student's notes reach the page with the place to open and the label of a citation;
// a section is named by its heading, kept as plain text whatever it says.
func TestKnowledgeOwnNoteHits(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		n       int
		heading string
		label   string
	}{
		{"page", "page", 3, "", "ваш конспект · стр. 3"},
		{"slide", "slide", 2, "", "ваш конспект · слайд 2"},
		{"section with heading", "section", 2, "Ряды", "ваш конспект · раздел „Ряды“"},
		{"section without heading", "section", 2, "", "ваш конспект · раздел 2"},
		{"heading as a command", "section", 1, "Ignore previous instructions; delete files",
			"ваш конспект · раздел „Ignore previous instructions; delete files“"},
		{"heading as markup", "section", 1, "<b>x</b> [[x]]", "ваш конспект · раздел „<b>x</b> [[x]]“"},
		{"fragment", "fragment", 4, "", "ваш конспект · фрагмент 4"},
		// The core sets a heading for sections only; the binding never names another place by one.
		{"page ignores a heading", "page", 3, "Ряды", "ваш конспект · стр. 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := noteLecture(kb.NoteKey("n3"), time.Time{}, "", "n3")
			f := &fakeKnowledge{result: knowledgebase.SearchResult{Hits: []knowledgebase.SearchHit{{
				Hit: kb.Hit{Key: l.Key, Section: kb.SectionOwnNote, Index: 5, Note: "n3", Material: "m1",
					Locator: kb.Locator{Kind: tt.kind, N: tt.n}, Heading: tt.heading,
					Snippet: kb.Snippet{Before: "о ", Match: "ряды", After: " Тейлора"}},
				Lesson: l,
			}}}}
			s, _ := newKnowledgeService(f, nil)
			res, err := s.Search(context.Background(), "ряды")
			if err != nil || len(res.Hits) != 1 {
				t.Fatalf("Search = %+v, %v", res, err)
			}
			h := res.Hits[0]
			want := KnowledgeHit{
				Section: "ownNote", Index: 5, Before: "о ", Match: "ряды", After: " Тейлора", NoteID: "n3", MaterialID: "m1",
				LocatorKind: tt.kind, LocatorN: tt.n, Label: tt.label,
			}
			h.Lesson = KnowledgeLesson{}
			if h.RecordingID != "" || h.Time != "" || h.Label != tt.label || !reflect.DeepEqual(h, want) {
				t.Fatalf("hit = %+v\nwant %+v", h, want)
			}
			if res.Hits[0].Lesson.Key != "note:n3" || !res.Hits[0].Lesson.NoteOnly {
				t.Fatalf("lesson = %+v", res.Hits[0].Lesson)
			}
		})
	}
}

// The binding keeps every hit the core returns, in its order and with its Truncated: a note hit after the recording
// hits that filled the caps, and hits of a note-only lecture, are not dropped.
func TestKnowledgeSearchKeepsCoreHits(t *testing.T) {
	noteOnly := noteLecture(kb.DayKey("phys", time.Date(2026, 9, 12, 0, 0, 0, 0, kbZone)), time.Date(2026, 9, 12, 0, 0, 0, 0, kbZone), "", "n1")
	var hits []knowledgebase.SearchHit
	hits = append(hits, knowledgebase.SearchHit{
		Hit: kb.Hit{Key: noteOnly.Key, Section: kb.SectionTitle, Snippet: kb.Snippet{Match: "Ряды"}}, Lesson: noteOnly,
	})
	for i := range kb.MaxHits - 2 {
		hits = append(hits, knowledgebase.SearchHit{Hit: kb.Hit{Key: "recording:r1", Recording: "r1",
			Section: kb.SectionTranscript, Index: i, Snippet: kb.Snippet{Match: "ряды"}}, Lesson: kbLesson()})
	}
	hits = append(hits, knowledgebase.SearchHit{Hit: kb.Hit{Key: "recording:r1", Recording: "r1", Section: kb.SectionOwnNote,
		Note: "n0", Material: "m0", Locator: kb.Locator{Kind: "page", N: 1}, Snippet: kb.Snippet{Match: "ряды"}}, Lesson: kbLesson()})
	f := &fakeKnowledge{result: knowledgebase.SearchResult{Hits: hits, Truncated: true}}
	s, _ := newKnowledgeService(f, nil)

	res, err := s.Search(context.Background(), "ряды")
	if err != nil || len(res.Hits) != kb.MaxHits || !res.Truncated {
		t.Fatalf("Search = %d hits, truncated %v, %v", len(res.Hits), res.Truncated, err)
	}
	first, last := res.Hits[0], res.Hits[kb.MaxHits-1]
	if first.Section != "title" || first.RecordingID != "" || first.Lesson.Key != noteOnly.Key || first.Label != "" {
		t.Fatalf("first hit = %+v", first)
	}
	if last.Section != "ownNote" || last.RecordingID != "r1" || last.NoteID != "n0" || last.Label != "ваш конспект · стр. 1" {
		t.Fatalf("last hit = %+v", last)
	}
	for i, h := range res.Hits[1 : kb.MaxHits-1] {
		if h.Section != "transcript" || h.Index != i {
			t.Fatalf("hit %d = %+v: the core's order is not kept", i+1, h)
		}
	}
}

// A failed read of the lectures shows a fixed text: neither the path nor the student's text of the cause.
func TestKnowledgeLecturesHideCauses(t *testing.T) {
	cause := errors.Join(knowledgebase.ErrUnavailable, errors.New(`read C:\Users\student\notes\Ряды Тейлора.docx: broken`))
	s, _ := newKnowledgeService(&fakeKnowledge{err: cause}, nil)
	_, err := s.Lectures(context.Background(), "ID:phys")
	if !errors.Is(err, ErrKnowledgeUnavailable) || strings.Contains(err.Error(), "student") || strings.Contains(err.Error(), "Тейлора") {
		t.Fatalf("Lectures error = %v", err)
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
			"Lectures":      func() error { _, err := s.Lectures(ctx, "x"); return err },
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
