package knowledgebase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// memNotes is the notes table; cancel, if set, is called when it is listed.
type memNotes struct {
	rows       []*note.Note
	unreadable []shared.NoteID
	err        error
	cancel     func()
}

func (m *memNotes) ListAll(context.Context) ([]*note.Note, []shared.NoteID, error) {
	if m.cancel != nil {
		m.cancel()
	}
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.rows, m.unreadable, nil
}

// partText is what the library answers for one material.
type partText struct {
	m    *material.Material
	text material.Text
	err  error
}

// fakeTexts is the library's MaterialText; cancel, if set, is called on the read of material cancelAt.
type fakeTexts struct {
	parts    map[shared.MaterialID]partText
	read     []shared.MaterialID
	cancelAt shared.MaterialID
	cancel   func()
}

func (f *fakeTexts) MaterialText(ctx context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	f.read = append(f.read, id)
	if id == f.cancelAt && f.cancel != nil {
		f.cancel()
		return nil, material.Text{}, ctx.Err()
	}
	p, ok := f.parts[id]
	if !ok {
		return nil, material.Text{}, fmt.Errorf("material %s: %w", id, shared.ErrNotFound)
	}
	return p.m, p.text, p.err
}

type fakeSubjects struct {
	list []*subject.Subject
	err  error
}

func (f *fakeSubjects) Subjects(context.Context) ([]*subject.Subject, error) { return f.list, f.err }

type notesEnv struct {
	*env
	notes    *memNotes
	texts    *fakeTexts
	subjects *fakeSubjects
}

// newNotesEnv is the base with a library of «Физика» (phys) and «Химия» (chem), whose strings resolve to them.
func newNotesEnv(t *testing.T) *notesEnv {
	t.Helper()
	e := &notesEnv{env: newEnv(t), notes: &memNotes{}, texts: &fakeTexts{parts: map[shared.MaterialID]partText{}}}
	var subs []*subject.Subject
	for _, s := range []struct {
		id   shared.SubjectID
		name string
	}{{"phys", "Физика"}, {"chem", "Химия"}} {
		sub, err := subject.New(subject.Spec{ID: s.id, Name: s.name, Semester: libvalues.SemesterOf(t0)}, t0)
		if err != nil {
			t.Fatal(err)
		}
		subs = append(subs, sub)
	}
	e.subjects = &fakeSubjects{list: subs}
	e.svc.library = &fakeResolver{table: map[string]subjectmatch.Match{"Физика": {ID: "phys", Name: "Физика"}}}
	e.svc.notes, e.svc.noteTexts, e.svc.subjects = e.notes, e.texts, e.subjects
	return e
}

func noteDay(t *testing.T, s string) note.Date {
	t.Helper()
	d, err := note.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// addNote stores an active note of sub made made after t0.
func (e *notesEnv) addNote(t *testing.T, id shared.NoteID, sub shared.SubjectID, spec note.AnchorSpec, made time.Duration, parts ...shared.MaterialID) *note.Note {
	t.Helper()
	a, err := note.NewAnchor(spec)
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.New(note.Spec{ID: id, Subject: sub, Anchor: a, Title: "Конспект " + string(id), Parts: parts}, t0.Add(made))
	if err != nil {
		t.Fatal(err)
	}
	e.notes.rows = append(e.notes.rows, n)
	return n
}

// addPart stores a text material of sub whose text is the sections given (each its lines).
func (e *notesEnv) addPart(t *testing.T, id shared.MaterialID, sub shared.SubjectID, sections ...[]string) {
	t.Helper()
	data := []byte("content of " + string(id))
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf(data), Size: int64(len(data)), Type: material.TypeTXT, Name: string(id) + ".md"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Place(sub, shared.FolderID("lectures-"+string(sub)), t0); err != nil {
		t.Fatal(err)
	}
	frags := make([]material.Fragment, len(sections))
	for i, lines := range sections {
		frags[i] = material.Fragment{Locator: material.Locator{Kind: material.LocatorSection, N: i + 1}, Lines: lines}
	}
	text, err := material.NewText(frags, false)
	if err != nil {
		t.Fatal(err)
	}
	e.texts.parts[id] = partText{m: m, text: text}
}

func keysOf(ls []kb.Lesson) []string {
	out := []string{}
	for _, l := range ls {
		out = append(out, l.Key)
	}
	return out
}

func noteIDsOf(l kb.Lesson) []shared.NoteID {
	var out []shared.NoteID
	for _, n := range l.Notes {
		out = append(out, n.ID)
	}
	return out
}

// seedLectures stores the recording r1 of «Физика» (lesson "lesson-r1") and notes on every kind of anchor.
func (e *notesEnv) seedLectures(t *testing.T) {
	t.Helper()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
	e.schedule["l-past"] = e.schedule["lesson-r1"] // a schedule lesson without a recording
	sep12 := noteDay(t, "2026-09-12")
	e.addPart(t, "m1", "phys", []string{"Ряды", "Ряд Тейлора сходится"})

	e.addNote(t, "on-rec", "phys", note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: sep12}, 0, "m1")
	e.addNote(t, "on-lesson", "phys", note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "lesson-r1", Date: sep12}, time.Minute, "m1")
	e.addNote(t, "past", "phys", note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l-past", Date: noteDay(t, "2026-09-05"),
		LessonKind: "seminar", Title: "Пределы"}, 2*time.Minute, "m1")
	e.addNote(t, "gone", "phys", note.AnchorSpec{Kind: note.AnchorLesson, Lesson: "l-gone", Date: sep12}, 3*time.Minute, "m1")
	e.addNote(t, "day", "phys", note.AnchorSpec{Kind: note.AnchorDay, Date: sep12}, 4*time.Minute, "m1")
	e.addNote(t, "undated", "phys", note.AnchorSpec{Kind: note.AnchorUndated}, 5*time.Minute, "m1")
	removed := e.addNote(t, "removed", "phys", note.AnchorSpec{Kind: note.AnchorDay, Date: noteDay(t, "2026-09-01")}, 6*time.Minute, "m1")
	if _, err := removed.Remove(t0); err != nil {
		t.Fatal(err)
	}
	e.addNote(t, "unknown-subject", "bio", note.AnchorSpec{Kind: note.AnchorUndated}, 7*time.Minute, "m1")
	e.addNote(t, "chem", "chem", note.AnchorSpec{Kind: note.AnchorUndated}, 8*time.Minute, "m1")
}

func TestLecturesWithNotes(t *testing.T) {
	e, ctx := newNotesEnv(t), context.Background()
	e.seedLectures(t)
	e.notes.unreadable = []shared.NoteID{"broken"}

	got, err := e.svc.Lectures(ctx, kb.LibraryKey("phys"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"recording:r1", "day:phys:2026-09-12", "lesson:l-past", "note:undated"}; !reflect.DeepEqual(keysOf(got), want) {
		t.Fatalf("Lectures(phys) = %v\nwant %v", keysOf(got), want)
	}
	if ids := noteIDsOf(got[0]); !reflect.DeepEqual(ids, []shared.NoteID{"on-rec", "on-lesson"}) {
		t.Fatalf("notes of r1 = %v", ids)
	}
	if ids := noteIDsOf(got[1]); !reflect.DeepEqual(ids, []shared.NoteID{"gone", "day"}) {
		t.Fatalf("notes of the day = %v", ids)
	}
	sep12 := time.Date(2026, 9, 12, 0, 0, 0, 0, msk)
	if !got[1].StartedAt.Equal(sep12) || got[1].StartedAt.Location() != msk || got[2].Title != "Пределы" || got[2].Kind != "seminar" ||
		!got[3].Undated() || got[3].Library.Name != "Физика" {
		t.Fatalf("note-only lectures = %+v", got[1:])
	}
	for _, want := range []string{opNotesUnreadable, opNoteSubject} {
		if !strings.Contains(e.logs.String(), want) {
			t.Errorf("not logged: %q in %s", want, e.logs)
		}
	}
	if strings.Contains(e.logs.String(), "Конспект") {
		t.Errorf("a note title is logged: %s", e.logs)
	}

	// Lessons stays the recorded lessons as before the notes; Subjects lists the subject of notes only.
	lessons, err := e.svc.Lessons(ctx, kb.LibraryKey("phys"))
	if err != nil || !reflect.DeepEqual(keysOf(lessons), []string{"recording:r1"}) || lessons[0].Notes != nil {
		t.Fatalf("Lessons(phys) = %+v, %v", lessons, err)
	}
	subjects, err := e.svc.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for _, s := range subjects {
		rows = append(rows, fmt.Sprintf("%s %s %d", s.Key, s.Name, s.Lessons))
	}
	if want := []string{"ID:phys Физика 1", "ID:chem Химия 0"}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("Subjects = %v, want %v", rows, want)
	}
	if name, err := e.svc.SubjectFileName(ctx, kb.LibraryKey("chem")); err != nil || name != "Химия.md" {
		t.Fatalf("SubjectFileName(chem) = %q, %v", name, err)
	}
}

func TestSearchNotes(t *testing.T) {
	e, ctx := newNotesEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика"})
	e.addPart(t, "good", "phys", []string{"Ряды", "Ряд Тейлора сходится"}, []string{"Другое"})
	e.addPart(t, "other-subject", "chem", []string{"Тейлор из химии"})
	e.addPart(t, "removed", "phys", []string{"Тейлор удалён"})
	e.texts.parts["removed"] = partText{err: fmt.Errorf("library: removed: %w", material.ErrRemoved)}
	e.texts.parts["pdf"] = partText{err: fmt.Errorf("library: no text: %w", material.ErrTextNotExtracted)}
	e.texts.parts["damaged"] = partText{err: fmt.Errorf("library: damaged: %w", material.ErrDamaged)}
	e.addNote(t, "n1", "phys", note.AnchorSpec{Kind: note.AnchorRecording, Recording: "r1", Date: noteDay(t, "2026-10-19")}, 0,
		"removed", "pdf", "damaged", "other-subject", "good")
	e.addNote(t, "n2", "phys", note.AnchorSpec{Kind: note.AnchorUndated}, time.Minute, "good")

	res, err := e.svc.Search(ctx, "тейлор")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, fmt.Sprintf("%s %s %s %s %s/%d %s", h.Lesson.Key, h.Section, h.Note, h.Material, h.Locator.Kind, h.Locator.N, h.Snippet.Match))
	}
	want := []string{
		"recording:r1 ownNote n1 good section/1 Тейлор",
		"note:n2 ownNote n2 good section/1 Тейлор",
	}
	if !reflect.DeepEqual(got, want) || res.Truncated {
		t.Fatalf("hits = %v (truncated %v)\nwant %v", got, res.Truncated, want)
	}
	logs := e.logs.String()
	if strings.Count(logs, opNotePart) != 1 || strings.Count(logs, opNotePartSubject) != 1 || strings.Contains(logs, "removed") ||
		strings.Contains(logs, "material=pdf") || strings.Contains(logs, "Тейлор") {
		t.Fatalf("logs = %s", logs)
	}
}

func TestTextsAndExportWithNotes(t *testing.T) {
	e, ctx := newNotesEnv(t), context.Background()
	e.seedLectures(t)

	texts, err := e.svc.Texts(ctx, "phys")
	if err != nil {
		t.Fatal(err)
	}
	// The answers cite the student's notes (TASK-153): Texts gives the lessons of Lectures with the note texts read.
	var keys []string
	for _, lt := range texts {
		keys = append(keys, lt.Lesson.Key)
		if len(lt.Document.Notes) == 0 || lt.Document.Notes[0].Text != "Ряды\nРяд Тейлора сходится" {
			t.Fatalf("Texts(phys) %s: notes = %+v", lt.Lesson.Key, lt.Document.Notes)
		}
	}
	if want := []string{"recording:r1", "day:phys:2026-09-12", "lesson:l-past", "note:undated"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("Texts(phys) = %v\nwant %v", keys, want)
	}
	if texts[0].Document.Recording != "r1" || !reflect.DeepEqual(noteIDsOf(texts[0].Lesson), []shared.NoteID{"on-rec", "on-lesson"}) {
		t.Fatalf("Texts(phys)[0] = %+v", texts[0])
	}
	all, err := e.svc.Texts(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var allKeys []string
	for _, lt := range all {
		allKeys = append(allKeys, lt.Lesson.Key)
	}
	if len(all) != 5 || !strings.Contains(strings.Join(allKeys, " "), "note:chem") || strings.Contains(strings.Join(allKeys, " "), "removed") {
		t.Fatalf("Texts(all) = %v", allKeys)
	}

	if err := e.svc.ExportSubject(ctx, kb.LibraryKey("phys"), `C:\out\Физика.md`); err != nil {
		t.Fatal(err)
	}
	out := e.writer.content
	order := []string{
		"# Физика\n",
		"## 05\\.09.2026 · семинар · Пределы\n", // oldest first
		"## 12\\.09.2026 · Конспект gone\n",
		"## 19\\.10.2026, 10:00 · лекция · Механика\n",
		"### Текст лекции", "### Мой конспект", "#### Конспект on-rec", "#### Конспект on-lesson",
		"## Без даты · Конспект undated\n", // undated last
	}
	at := 0
	for _, s := range order {
		i := strings.Index(out[at:], s)
		if i < 0 {
			t.Fatalf("export lacks %q after byte %d:\n%s", s, at, out)
		}
		at += i + len(s)
	}
	if n := strings.Count(out, "### Мой конспект"); n != 4 || strings.Contains(out, "removed") || strings.Count(out, "Ряд Тейлора сходится") != 6 {
		t.Fatalf("export = %s", out)
	}
}

func TestNotesFailures(t *testing.T) {
	ctx := context.Background()

	// Notes or subjects that cannot be listed leave the recorded lessons, logged without texts.
	for name, tc := range map[string]struct {
		fail func(*notesEnv)
		op   string
	}{
		"notes not listed":    {func(e *notesEnv) { e.notes.err = errDisk }, opNotesNotListed},
		"subjects not listed": {func(e *notesEnv) { e.subjects.err = errDisk }, opNoteSubjects},
	} {
		t.Run(name, func(t *testing.T) {
			e := newNotesEnv(t)
			e.seedLectures(t)
			tc.fail(e)
			recorded := []string{"recording:r1"}
			if got, err := e.svc.Lectures(ctx, kb.LibraryKey("phys")); err != nil || !reflect.DeepEqual(keysOf(got), recorded) {
				t.Fatalf("Lectures = %v, %v", keysOf(got), err)
			}
			subs, err := e.svc.Subjects(ctx)
			if err != nil || len(subs) != 1 || subs[0].Key != kb.LibraryKey("phys") {
				t.Fatalf("Subjects = %+v, %v", subs, err)
			}
			res, err := e.svc.Search(ctx, "механика")
			if err != nil || len(res.Hits) == 0 || res.Hits[0].Lesson.Key != "recording:r1" {
				t.Fatalf("Search = %+v, %v", res, err)
			}
			if name, err := e.svc.SubjectFileName(ctx, kb.LibraryKey("phys")); err != nil || name != "Физика.md" {
				t.Fatalf("SubjectFileName = %q, %v", name, err)
			}
			if _, err := e.svc.SubjectFileName(ctx, kb.LibraryKey("chem")); !errors.Is(err, ErrSubjectNotFound) {
				t.Fatalf("SubjectFileName(notes only) = %v", err)
			}
			if err := e.svc.ExportSubject(ctx, kb.LibraryKey("phys"), `C:\out\Физика.md`); err != nil ||
				strings.Contains(e.writer.content, "Мой конспект") || !strings.Contains(e.writer.content, "Механика") {
				t.Fatalf("ExportSubject = %v:\n%s", err, e.writer.content)
			}
			logs := e.logs.String()
			if !strings.Contains(logs, tc.op) || strings.Contains(logs, "Конспект") || strings.Contains(logs, "Ряд") {
				t.Fatalf("logs = %s", logs)
			}
		})
	}

	// A cancelled listing of the notes fails.
	e := newNotesEnv(t)
	e.seedLectures(t)
	cancelledList, cancelList := context.WithCancel(ctx)
	defer cancelList()
	e.notes.err, e.notes.cancel = context.Canceled, cancelList
	if _, err := e.svc.Subjects(cancelledList); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled listing: %v", err)
	}

	// A cancelled read of a part fails the search; a failed read of a lesson only keeps the lesson.
	e = newNotesEnv(t)
	e.seedLectures(t)
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	e.texts.cancelAt, e.texts.cancel = "m1", cancel
	if _, err := e.svc.Search(cancelled, "ряды"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}

	e = newNotesEnv(t)
	e.seedLectures(t)
	failing := &failingSchedule{memSchedule: e.schedule, fail: "l-gone"}
	e.svc.schedule = failing
	got, err := e.svc.Lectures(ctx, kb.LibraryKey("phys"))
	if err != nil || !reflect.DeepEqual(keysOf(got), []string{"recording:r1", "day:phys:2026-09-12", "lesson:l-gone", "lesson:l-past", "note:undated"}) {
		t.Fatalf("with a failing schedule = %v, %v", keysOf(got), err)
	}
	if !strings.Contains(e.logs.String(), opNoteLesson) {
		t.Fatalf("not logged: %s", e.logs)
	}

	for name, d := range map[string]Deps{
		"store only": {Notes: e.notes},
		"no texts":   {Notes: e.notes, Subjects: e.subjects},
		"no store":   {NoteTexts: e.texts, Subjects: e.subjects},
	} {
		d.Lessons, d.Recordings, d.Schedule, d.Transcripts, d.Artifacts = e.recs, e.recs, e.schedule, e.transcripts, e.artifacts
		d.Summaries, d.LectureTexts, d.Writer, d.Clock = e.summaries, e.lectures, e.writer, e.svc.clock
		if _, err := New(d); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: New = %v", name, err)
		}
	}
}

// failingSchedule fails the read of one lesson.
type failingSchedule struct {
	memSchedule
	fail shared.LessonID
}

func (f *failingSchedule) Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if id == f.fail {
		return nil, errDisk
	}
	return f.memSchedule.Get(ctx, id)
}
