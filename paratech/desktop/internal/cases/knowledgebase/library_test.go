package knowledgebase

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/shared"
)

// fakeResolver resolves by an exact table and records what it was asked.
type fakeResolver struct {
	table map[string]subjectmatch.Match
	asked [][]string
	err   error
}

func (f *fakeResolver) ResolveSubjects(_ context.Context, names []string) (map[string]subjectmatch.Match, error) {
	f.asked = append(f.asked, slices.Sorted(slices.Values(names)))
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]subjectmatch.Match{}
	for _, n := range names {
		if m, ok := f.table[n]; ok {
			out[n] = m
		}
	}
	return out, nil
}

// Lessons whose string resolves to a library subject are grouped by its id; the others keep their free-string group
// (ADR-0024, TASK-121). Nothing stored changes.
func TestSubjectsGroupedByLibrary(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	lib := &fakeResolver{table: map[string]subjectmatch.Match{
		"Физика":       {ID: "phys", Name: "Физика"},
		"Общая физика": {ID: "phys", Name: "Физика"},
	}}
	e.svc.library = lib
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика"})
	e.add(t, lessonSpec{id: "r2", start: time.Hour, title: "Оптика", subject: "Общая физика"})
	e.add(t, lessonSpec{id: "r3", start: 2 * time.Hour, title: "Реакции", subject: "Химия"})
	e.add(t, lessonSpec{id: "r4", start: 3 * time.Hour}) // ad hoc: never resolved

	subjects, err := e.svc.Subjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range subjects {
		keys = append(keys, s.Key)
	}
	if want := []string{kb.LibraryKey("phys"), "химия", ""}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("subject keys = %q, want %q", keys, want)
	}
	if subjects[0].Name != "Физика" || subjects[0].Lessons != 2 || subjects[0].SubjectID != "phys" {
		t.Fatalf("library subject %+v", subjects[0])
	}
	if got := lib.asked[0]; !reflect.DeepEqual(got, []string{"Общая физика", "Физика", "Химия"}) {
		t.Fatalf("asked to resolve %q", got)
	}

	lessons, err := e.svc.Lessons(ctx, kb.LibraryKey("phys"))
	if err != nil || !reflect.DeepEqual(recIDs(lessons), []shared.RecordingID{"r2", "r1"}) {
		t.Fatalf("Lessons(phys) = %v, %v", recIDs(lessons), err)
	}
	if lessons[0].Subject != "Общая физика" {
		t.Fatalf("the lesson's own subject changed: %q", lessons[0].Subject)
	}
	if name, err := e.svc.SubjectFileName(ctx, kb.LibraryKey("phys")); err != nil || name != "Физика.md" {
		t.Fatalf("SubjectFileName = %q, %v", name, err)
	}
	one, err := e.svc.Lesson(ctx, "r1")
	if err != nil || one.SubjectKey() != kb.LibraryKey("phys") {
		t.Fatalf("Lesson(r1) = %+v, %v", one, err)
	}

	// A correction that names the subject by an alias joins its group at once.
	got, err := e.svc.SetLessonInfo(ctx, "r3", "Общая физика", "")
	if err != nil || got.SubjectKey() != kb.LibraryKey("phys") {
		t.Fatalf("SetLessonInfo = %+v, %v", got, err)
	}

	lib.err = errors.New("db")
	if _, err := e.svc.Subjects(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Subjects with a failing library = %v", err)
	}
}
