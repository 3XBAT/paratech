package knowledgebase

import (
	"reflect"
	"testing"
	"time"
	"unicode"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func TestNewLesson(t *testing.T) {
	snapshot := &Source{Title: " Механика ", Subject: "Физика", Kind: "lecture"}
	live := &Source{Title: "Живое занятие", Subject: "Химия", Kind: "seminar"}
	tests := []struct {
		name                      string
		facts                     Facts
		title, subject, kind      string
		subjectEdited, kindEdited bool
	}{
		{name: "ad-hoc recording", facts: Facts{}},
		{name: "snapshot wins over the live lesson", facts: Facts{Snapshot: snapshot, Live: live},
			title: "Механика", subject: "Физика", kind: "lecture"},
		{name: "live lesson without a snapshot", facts: Facts{Live: live}, title: "Живое занятие", subject: "Химия", kind: "seminar"},
		{name: "live unknown kind", facts: Facts{Live: &Source{Subject: "Химия", Kind: "unknown"}}, subject: "Химия"},
		{name: "snapshot without a title takes the live title", facts: Facts{Snapshot: &Source{Subject: "Физика"}, Live: live},
			title: "Живое занятие", subject: "Физика"},
		{name: "correction wins per field", facts: Facts{Snapshot: snapshot, Correction: Source{Subject: " Матанализ "}},
			title: "Механика", subject: "Матанализ", kind: "lecture", subjectEdited: true},
		{name: "corrected kind only", facts: Facts{Snapshot: snapshot, Correction: Source{Kind: "lab"}},
			title: "Механика", subject: "Физика", kind: "lab", kindEdited: true},
		{name: "correction of an ad-hoc recording", facts: Facts{Correction: Source{Subject: "Физика", Kind: "practice"}},
			subject: "Физика", kind: "practice", subjectEdited: true, kindEdited: true},
		{name: "kind that is no code is dropped", facts: Facts{Snapshot: &Source{Kind: "<b>"}, Correction: Source{Kind: "x"}}},
		{name: "untrusted text is one line", facts: Facts{Snapshot: &Source{Title: "a\u202e\nb", Subject: "c\td"}},
			title: "a b", subject: "c d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.facts.Recording, tc.facts.StartedAt = "r1", t0
			l := NewLesson(tc.facts)
			if l.Title != tc.title || l.Subject != tc.subject || l.Kind != tc.kind ||
				l.SubjectEdited != tc.subjectEdited || l.KindEdited != tc.kindEdited {
				t.Fatalf("lesson = %+v; want title %q, subject %q, kind %q, edited %v/%v",
					l, tc.title, tc.subject, tc.kind, tc.subjectEdited, tc.kindEdited)
			}
			if l.Recording != "r1" || !l.StartedAt.Equal(t0) {
				t.Fatalf("lesson = %+v", l)
			}
		})
	}
}

func TestSubjectKey(t *testing.T) {
	tests := []struct{ a, b string }{
		{"Физика", " физика "},
		{"ФИЗИКА", "физика"},
		{"Ёмкость", "емкость"},
		{"Мат  анализ", "мат анализ"},
	}
	for _, tc := range tests {
		if SubjectKey(tc.a) != SubjectKey(tc.b) {
			t.Errorf("SubjectKey(%q) = %q, SubjectKey(%q) = %q; want equal", tc.a, SubjectKey(tc.a), tc.b, SubjectKey(tc.b))
		}
	}
	if SubjectKey("Физика") == SubjectKey("Химия") || SubjectKey("  ") != "" {
		t.Error("different subjects share a key, or spaces are a subject")
	}
}

func lessonAt(id shared.RecordingID, subject string, at time.Time) Lesson {
	return Lesson{Recording: id, Subject: subject, StartedAt: at}
}

func TestSubjects(t *testing.T) {
	lessons := []Lesson{
		lessonAt("r1", "физика", t0),
		lessonAt("r2", "", t0.Add(time.Hour)),
		lessonAt("r3", "Физика", t0.Add(2*time.Hour)),
		lessonAt("r4", "Алгебра", t0.Add(-time.Hour)),
	}
	got := Subjects(lessons)
	want := []Subject{
		{Key: "алгебра", Name: "Алгебра", Lessons: 1, Latest: t0.Add(-time.Hour)},
		{Key: "физика", Name: "Физика", Lessons: 2, Latest: t0.Add(2 * time.Hour)}, // spelled as the newest lesson
		{Key: "", Name: NoSubjectName, Lessons: 1, Latest: t0.Add(time.Hour), NoSubject: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Subjects =\n %+v\nwant\n %+v", got, want)
	}
	if got := Subjects(nil); len(got) != 0 {
		t.Fatalf("Subjects(nil) = %+v", got)
	}
}

func TestLessonsOf(t *testing.T) {
	lessons := []Lesson{
		lessonAt("r1", "Физика", t0),
		lessonAt("r2", "", t0.Add(time.Hour)),
		lessonAt("r3", "физика", t0.Add(2*time.Hour)),
		lessonAt("r0", "Физика", t0),
	}
	ids := func(ls []Lesson) []shared.RecordingID {
		var out []shared.RecordingID
		for _, l := range ls {
			out = append(out, l.Recording)
		}
		return out
	}
	if got := ids(LessonsOf(lessons, "ФИЗИКА")); !reflect.DeepEqual(got, []shared.RecordingID{"r3", "r0", "r1"}) {
		t.Fatalf("LessonsOf(физика) = %v", got)
	}
	if got := ids(LessonsOf(lessons, "")); !reflect.DeepEqual(got, []shared.RecordingID{"r2"}) {
		t.Fatalf("LessonsOf(no subject) = %v", got)
	}
	if got := LessonsOf(lessons, "Химия"); len(got) != 0 {
		t.Fatalf("LessonsOf(unknown) = %v", got)
	}
	if got := ids(Oldest(lessons)); !reflect.DeepEqual(got, []shared.RecordingID{"r1", "r0", "r2", "r3"}) {
		t.Fatalf("Oldest = %v", got)
	}
	if lessons[0].Recording != "r1" {
		t.Fatal("sorting changed the input")
	}
}

// fold is unicode.ToLower with «ё» as «е» and spaces and controls as a space; its fast paths change nothing.
func TestFold(t *testing.T) {
	for r := rune(0); r <= 0x2FFFF; r++ {
		want := unicode.ToLower(r)
		switch {
		case r == 'ё' || r == 'Ё':
			want = 'е'
		case unicode.IsSpace(r) || unicode.IsControl(r):
			want = ' '
		}
		if got := fold(r); got != want {
			t.Fatalf("fold(%U) = %U, want %U", r, got, want)
		}
	}
}
