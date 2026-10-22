package knowledgebase

import (
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Strings that resolve to a library subject are grouped by its id and named as the library names it; the others stay
// grouped by their free string as before (ADR-0024).
func TestSubjectsGroupByLibrarySubject(t *testing.T) {
	lessons := WithLibrary([]Lesson{
		lessonAt("r1", "Физика", t0),
		lessonAt("r2", "Общая физика", t0.Add(time.Hour)), // an alias of the same subject
		lessonAt("r3", "История", t0.Add(2*time.Hour)),    // resolves to nothing
		lessonAt("r4", "", t0.Add(3*time.Hour)),
		lessonAt("r5", "ID:phys", t0.Add(4*time.Hour)), // a free string spelled like a key stays a free string
	}, map[string]LibrarySubject{
		"Физика":       {ID: "phys", Name: "Физика (курс)"},
		"Общая физика": {ID: "phys", Name: "Физика (курс)"},
		"":             {ID: "never", Name: "x"}, // the group without a subject is never resolved
	})
	if lessons[1].Subject != "Общая физика" {
		t.Fatalf("the stored string was changed: %q", lessons[1].Subject)
	}

	got := Subjects(lessons)
	want := []Subject{
		{Key: "id:phys", Name: "ID:phys", Lessons: 1, Latest: t0.Add(4 * time.Hour)},
		{Key: "история", Name: "История", Lessons: 1, Latest: t0.Add(2 * time.Hour)},
		{Key: LibraryKey("phys"), Name: "Физика (курс)", Lessons: 2, Latest: t0.Add(time.Hour), SubjectID: "phys"},
		{Key: "", Name: NoSubjectName, Lessons: 1, Latest: t0.Add(3 * time.Hour), NoSubject: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Subjects =\n %+v\nwant\n %+v", got, want)
	}

	ids := func(ls []Lesson) []shared.RecordingID {
		var out []shared.RecordingID
		for _, l := range ls {
			out = append(out, l.Recording)
		}
		return out
	}
	if got := ids(LessonsOf(lessons, LibraryKey("phys"))); !reflect.DeepEqual(got, []shared.RecordingID{"r2", "r1"}) {
		t.Fatalf("LessonsOf(phys) = %v", got)
	}
	// The free string of a resolved subject no longer has a group of its own.
	if got := LessonsOf(lessons, "физика"); len(got) != 0 {
		t.Fatalf("LessonsOf(физика) = %v", ids(got))
	}
	if got := ids(LessonsOf(lessons, "id:phys")); !reflect.DeepEqual(got, []shared.RecordingID{"r5"}) {
		t.Fatalf("LessonsOf(id:phys) = %v", got)
	}

	// Resolving again with nothing clears the library subject.
	if again := WithLibrary(lessons, nil); again[0].Library != (LibrarySubject{}) || again[0].SubjectKey() != "физика" {
		t.Fatalf("WithLibrary(nil) kept %+v", again[0].Library)
	}
}
