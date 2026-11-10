package knowledgebase

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// newFoldersEnv is the base with the library of newNotesEnv; «Химия» resolves to chem and «Матан» to several subjects.
func newFoldersEnv(t *testing.T) *notesEnv {
	t.Helper()
	e := newNotesEnv(t)
	e.svc.library = &fakeResolver{
		table: map[string]subjectmatch.Match{
			"Физика": {ID: "phys", Name: "Физика"},
			"Химия":  {ID: "chem", Name: "Химия"},
		},
		ambiguous: []string{"Матан"},
	}
	return e
}

// TASK-429: a recorded lesson of a subject is in the system folder its kind names (the correction wins); lessons of
// other subjects, of no library subject or of several are in none.
func TestFolderLessons(t *testing.T) {
	tests := []struct {
		name    string
		spec    lessonSpec
		correct string // a kind the user set; "" none
		want    subject.System
	}{
		{name: "lecture", spec: lessonSpec{subject: "Физика", kind: "lecture"}, want: subject.SystemLectures},
		{name: "seminar", spec: lessonSpec{subject: "Физика", kind: "seminar"}, want: subject.SystemSeminars},
		{name: "practice", spec: lessonSpec{subject: "Физика", kind: "practice"}, want: subject.SystemSeminars},
		{name: "lab", spec: lessonSpec{subject: "Физика", kind: "lab"}, want: subject.SystemLabs},
		{name: "no kind", spec: lessonSpec{subject: "Физика"}, want: subject.SystemLectures},
		{name: "unknown kind", spec: lessonSpec{subject: "Физика", kind: "workshop"}, want: subject.SystemLectures},
		{name: "live lesson only", spec: lessonSpec{subject: "Физика", kind: "lab", live: true}, want: subject.SystemLabs},
		{name: "corrected kind", spec: lessonSpec{subject: "Физика", kind: "lecture"}, correct: "lab", want: subject.SystemLabs},
		{name: "another subject", spec: lessonSpec{subject: "Химия", kind: "lecture"}},
		{name: "none", spec: lessonSpec{subject: "Биология", kind: "lecture"}},
		{name: "ambiguous", spec: lessonSpec{subject: "Матан", kind: "lecture"}},
		{name: "no subject", spec: lessonSpec{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ctx := newFoldersEnv(t), context.Background()
			tt.spec.id, tt.spec.title = "r1", "Механика"
			if tt.spec.subject == "" {
				tt.spec.title = ""
			}
			e.add(t, tt.spec)
			if tt.correct != "" {
				if _, err := e.svc.SetLessonInfo(ctx, "r1", "", tt.correct); err != nil {
					t.Fatal(err)
				}
			}

			got, err := e.svc.FolderLessons(ctx, "phys")
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if got == nil || len(got) != 0 {
					t.Fatalf("FolderLessons = %+v, want an empty list", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("FolderLessons = %+v, want one row", got)
			}
			if got[0].System != tt.want || got[0].Folder != subject.SystemFolderID("phys", tt.want) ||
				got[0].Lesson.Recording != "r1" || got[0].Lesson.Notes != nil {
				t.Fatalf("row = %+v, want %s", got[0], tt.want)
			}
		})
	}
}

// The rows come in the order of Lessons (newest first), each in its own folder; the student's notes are not joined.
func TestFolderLessonsOrder(t *testing.T) {
	e, ctx := newFoldersEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
	e.add(t, lessonSpec{id: "r2", start: time.Hour, title: "Задачи", subject: "Физика", kind: "seminar"})
	e.add(t, lessonSpec{id: "r3", start: 2 * time.Hour, title: "Реакции", subject: "Химия", kind: "lab"})

	got, err := e.svc.FolderLessons(ctx, "phys")
	if err != nil || len(got) != 2 {
		t.Fatalf("FolderLessons = %+v, %v", got, err)
	}
	if got[0].Lesson.Recording != "r2" || got[0].System != subject.SystemSeminars ||
		got[1].Lesson.Recording != "r1" || got[1].System != subject.SystemLectures {
		t.Fatalf("rows = %+v", got)
	}
	chem, err := e.svc.FolderLessons(ctx, "chem")
	if err != nil || len(chem) != 1 || chem[0].Folder != subject.SystemFolderID("chem", subject.SystemLabs) {
		t.Fatalf("FolderLessons(chem) = %+v, %v", chem, err)
	}
}

// An unknown or removed subject has no rows; an invalid id is refused; a failed read wraps ErrUnavailable.
func TestFolderLessonsFailures(t *testing.T) {
	ctx := context.Background()
	disk := errors.New(`open C:\Users\student\db: locked`)
	tests := []struct {
		name    string
		sub     shared.SubjectID
		prepare func(t *testing.T, e *notesEnv)
		wantErr error
	}{
		{name: "unknown subject", sub: "bio"},
		{name: "removed subject", sub: "phys", prepare: func(t *testing.T, e *notesEnv) {
			if _, err := e.subjects.list[0].Remove(t0); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "invalid id", sub: "a/b", wantErr: ErrInvalidRequest},
		{name: "empty id", sub: "", wantErr: ErrInvalidRequest},
		{name: "recordings not readable", sub: "phys", prepare: func(_ *testing.T, e *notesEnv) { e.recs.listErr = disk }, wantErr: ErrUnavailable},
		{name: "subjects not readable", sub: "phys", prepare: func(_ *testing.T, e *notesEnv) { e.subjects.err = disk }, wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newFoldersEnv(t)
			e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
			if tt.prepare != nil {
				tt.prepare(t, e)
			}
			got, err := e.svc.FolderLessons(ctx, tt.sub)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || got != nil {
					t.Fatalf("FolderLessons(%q) = %+v, %v; want %v", tt.sub, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("FolderLessons(%q) = %+v, %v; want an empty list", tt.sub, got, err)
			}
		})
	}
}

// Without a library wired no subject has folders.
func TestFolderLessonsWithoutLibrary(t *testing.T) {
	e := newEnv(t)
	e.add(t, lessonSpec{id: "r1", title: "Механика", subject: "Физика", kind: "lecture"})
	if got, err := e.svc.FolderLessons(context.Background(), "phys"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("FolderLessons = %+v, %v", got, err)
	}
}
