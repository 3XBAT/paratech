package knowledgebase

import (
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var (
	phys = LibrarySubject{ID: "phys", Name: "Физика"}
	chem = LibrarySubject{ID: "chem", Name: "Химия"}
	sep1 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sep5 = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
)

func TestKeys(t *testing.T) {
	tests := []struct{ got, want string }{
		{RecordingKey("r1"), "recording:r1"},
		{LessonKey("l1"), "lesson:l1"},
		{DayKey("phys", sep5.Add(13*time.Hour)), "day:phys:2026-09-05"},
		{NoteKey("n1"), "note:n1"},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("key %q, want %q", tc.got, tc.want)
		}
	}
	if l := NewLesson(Facts{Recording: "r1", ScheduleLesson: "l1", StartedAt: t0}); l.Key != "recording:r1" || l.ScheduleLesson != "l1" {
		t.Fatalf("NewLesson = %+v", l)
	}
}

func noteOn(id shared.NoteID, a NoteAnchor, made time.Duration) NoteFacts {
	return NoteFacts{
		Note: id, Title: "Конспект " + string(id), Subject: phys, Anchor: a, Parts: []shared.MaterialID{"m-" + shared.MaterialID(id)},
		CreatedAt: t0.Add(made), Day: sep5,
	}
}

func TestWithNotes(t *testing.T) {
	recorded := []Lesson{
		{Key: RecordingKey("r1"), Recording: "r1", ScheduleLesson: "l1", StartedAt: t0, Subject: "Физика", Library: phys},
		{Key: RecordingKey("r2"), Recording: "r2", ScheduleLesson: "l1", StartedAt: t0.Add(time.Hour), Library: phys},
	}

	onRec := noteOn("rec", AnchorRecording, 0)
	onRec.Recording = "r1"
	onLesson := noteOn("lsn", AnchorLesson, time.Minute) // l1 has recordings: joins the newest, r2
	onLesson.Lesson = "l1"
	goneRec := noteOn("gone", AnchorRecording, 2*time.Minute) // r9 is no lesson of the base: falls back to its day
	goneRec.Recording = "r9"
	pastLesson := noteOn("past", AnchorLesson, 3*time.Minute)
	pastLesson.Lesson, pastLesson.Day, pastLesson.Kind, pastLesson.LessonTitle = "l2", sep1, "seminar", " Ряды "
	pastLesson2 := noteOn("past2", AnchorLesson, 4*time.Minute)
	pastLesson2.Lesson, pastLesson2.Day = "l2", sep1
	lostLesson := noteOn("lost", AnchorLesson, 5*time.Minute) // the schedule lost l3: its day
	lostLesson.Lesson, lostLesson.LessonGone = "l3", true
	day := noteOn("day", AnchorDay, 6*time.Minute)
	otherDay := noteOn("chemday", AnchorDay, 7*time.Minute) // same day, another subject
	otherDay.Subject = chem
	undatedLate := noteOn("u-late", AnchorUndated, 9*time.Minute)
	undatedLate.Day = time.Time{}
	undatedEarly := noteOn("u-early", AnchorUndated, 8*time.Minute)
	undatedEarly.Day = time.Time{}
	noSubject := noteOn("nosub", AnchorDay, 0)
	noSubject.Subject = LibrarySubject{}

	got := WithNotes(recorded, []NoteFacts{
		undatedLate, day, otherDay, lostLesson, pastLesson2, pastLesson, goneRec, onLesson, onRec, undatedEarly, noSubject,
	})

	type row struct {
		key   string
		notes []shared.NoteID
		title string
		kind  string
		start time.Time
		sub   shared.SubjectID
	}
	var rows []row
	for _, l := range got {
		r := row{key: l.Key, title: l.Title, kind: l.Kind, start: l.StartedAt, sub: l.Library.ID}
		for _, n := range l.Notes {
			r.notes = append(r.notes, n.ID)
		}
		rows = append(rows, r)
	}
	want := []row{
		{key: "recording:r2", notes: []shared.NoteID{"lsn"}, start: t0.Add(time.Hour), sub: "phys"},
		{key: "recording:r1", notes: []shared.NoteID{"rec"}, start: t0, sub: "phys"},
		{key: "day:chem:2026-09-05", notes: []shared.NoteID{"chemday"}, title: "Конспект chemday", start: sep5, sub: "chem"},
		{key: "day:phys:2026-09-05", notes: []shared.NoteID{"gone", "lost", "day"}, title: "Конспект gone", start: sep5, sub: "phys"},
		{key: "lesson:l2", notes: []shared.NoteID{"past", "past2"}, title: "Ряды", kind: "seminar", start: sep1, sub: "phys"},
		{key: "note:u-early", notes: []shared.NoteID{"u-early"}, title: "Конспект u-early", sub: "phys"},
		{key: "note:u-late", notes: []shared.NoteID{"u-late"}, title: "Конспект u-late", sub: "phys"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("WithNotes =\n %+v\nwant\n %+v", rows, want)
	}
	if got[4].ScheduleLesson != "l2" || got[4].Subject != "Физика" || got[4].Recorded() || got[4].Undated() || !got[5].Undated() {
		t.Fatalf("lesson lecture %+v, undated %+v", got[4], got[5])
	}
	if n := got[1].Notes[0]; n.Subject != "phys" || !reflect.DeepEqual(n.Parts, []shared.MaterialID{"m-rec"}) || n.Title != "Конспект rec" {
		t.Fatalf("joined note %+v", n)
	}
	if recorded[0].Notes != nil {
		t.Fatal("WithNotes changed its input")
	}
	if got := WithNotes(recorded, nil); len(got) != 2 || got[0].Recording != "r2" {
		t.Fatalf("without notes = %+v", got)
	}
}

// Newest puts the newest dated lessons first and the undated ones last, as their first notes were made; Oldest keeps
// the undated ones last.
func TestOrderWithUndated(t *testing.T) {
	undated := func(key string, made time.Duration) Lesson {
		return Lesson{Key: key, Notes: []Note{{CreatedAt: t0.Add(made)}}}
	}
	lessons := []Lesson{
		undated("note:b", time.Hour),
		{Key: "recording:r1", Recording: "r1", StartedAt: t0},
		undated("note:a", 2*time.Hour),
		{Key: "day:phys:2026-09-05", StartedAt: sep5},
		undated("note:c", time.Hour),
		{Key: "recording:r0", Recording: "r0", StartedAt: t0},
	}
	keys := func(ls []Lesson) []string {
		var out []string
		for _, l := range ls {
			out = append(out, l.Key)
		}
		return out
	}
	if got, want := keys(Newest(lessons)), []string{"recording:r0", "recording:r1", "day:phys:2026-09-05", "note:b", "note:c", "note:a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Newest = %v\nwant %v", got, want)
	}
	if got, want := keys(Oldest(lessons)), []string{"day:phys:2026-09-05", "recording:r1", "recording:r0", "note:b", "note:c", "note:a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Oldest = %v\nwant %v", got, want)
	}
	if got := keys(Oldest(lessons[:1])); !reflect.DeepEqual(got, []string{"note:b"}) {
		t.Fatalf("Oldest(undated only) = %v", got)
	}
}

// A subject known only from notes is listed with no recorded lesson; the counts and the newest start are of the
// recorded lessons.
func TestSubjectsWithNotes(t *testing.T) {
	lessons := []Lesson{
		{Key: "recording:r1", Recording: "r1", StartedAt: t0, Subject: "Физика", Library: phys},
		{Key: "day:phys:2026-10-25", StartedAt: t0.Add(24 * time.Hour), Subject: "Физика", Library: phys},
		{Key: "note:n1", Subject: "Химия", Library: chem},
	}
	got := Subjects(lessons)
	want := []Subject{
		{Key: LibraryKey("phys"), Name: "Физика", Lessons: 1, Latest: t0, SubjectID: "phys"},
		{Key: LibraryKey("chem"), Name: "Химия", SubjectID: "chem"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Subjects =\n %+v\nwant\n %+v", got, want)
	}
	if got := LessonsOf(lessons, LibraryKey("phys")); len(got) != 2 || got[0].Key != "day:phys:2026-10-25" {
		t.Fatalf("LessonsOf(phys) = %+v", got)
	}
}
