package todayfeed

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
)

// The lesson cards carry the library subject of the lesson and the call link kept on it (ADR-0032, TASK-364); a
// lesson whose string names no subject has none and no place to keep a link.
func TestTodayLessonsCarrySubjectAndCallLink(t *testing.T) {
	w := day(t)
	w.addLesson("l-chem", "Химия", "Химия", at(21, 16, 0))
	w.subjects["Химия"] = subjectmatch.Match{ID: "subj-chem", Name: "Химия"}
	link, err := subject.ParseCallLink("https://meet.example.org/j/1?pwd=FakePwd42")
	if err != nil {
		t.Fatal(err)
	}
	w.callLinks["subj-math"] = link

	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lessons) != 3 {
		t.Fatalf("lessons = %+v", got.Lessons)
	}
	math, phys, chem := got.Lessons[0], got.Lessons[1], got.Lessons[2]
	if math.SubjectID != "subj-math" || math.SubjectName != "Математический анализ" || math.CallLink != link {
		t.Errorf("math = %q %q %v", math.SubjectID, math.SubjectName, math.CallLink)
	}
	if phys.SubjectID != "" || phys.SubjectName != "" || !phys.CallLink.IsZero() {
		t.Errorf("physics, no library subject = %q %q %v", phys.SubjectID, phys.SubjectName, phys.CallLink)
	}
	if chem.SubjectID != "subj-chem" || !chem.CallLink.IsZero() {
		t.Errorf("chemistry, no link = %q %v", chem.SubjectID, chem.CallLink)
	}
	if w.linkReads != 1 {
		t.Errorf("CallLinks read %d times, want once", w.linkReads)
	}
}

func TestTodayCallLinkErrors(t *testing.T) {
	// No lesson resolves to a subject: the links are not read.
	w := newWorld(t)
	w.addLesson("l1", "Физика", "Физика", at(21, 8, 50))
	w.failLinks = errDisk
	if got, err := w.screen().Today(context.Background()); err != nil || got.Lessons[0].SubjectID != "" {
		t.Fatalf("Today without subjects = %+v, %v", got.Lessons, err)
	}

	w.subjects["Физика"] = subjectmatch.Match{ID: "subj-phys", Name: "Физика"}
	if _, err := w.screen().Today(context.Background()); !errors.Is(err, errDisk) {
		t.Fatalf("Today with a failing link read = %v, want the storage error", err)
	}
}
