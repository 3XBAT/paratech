package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	libvalues "paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

type mentionLibrary struct {
	subjects  []*subject.Subject
	materials map[shared.MaterialID]*material.Material
	err       error
}

func (l mentionLibrary) Subjects(context.Context) ([]*subject.Subject, error) {
	return l.subjects, l.err
}

// Material answers as library.Service does: a malformed id, a missing one, a store failure.
func (l mentionLibrary) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	if id.Validate() != nil {
		return nil, library.ErrInvalidRequest
	}
	if l.err != nil {
		return nil, fmt.Errorf("%w: %w", library.ErrUnavailable, l.err)
	}
	m, ok := l.materials[id]
	if !ok {
		return nil, fmt.Errorf("%w: %w", library.ErrMaterialNotFound, shared.ErrNotFound)
	}
	return m, nil
}

type mentionLessons map[shared.LessonID]*lesson.Lesson

func (m mentionLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if l, ok := m[id]; ok {
		return l, nil
	}
	return nil, shared.ErrNotFound
}

type mentionAssignments map[shared.AssignmentID]bool // true: deleted

func (m mentionAssignments) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	deleted, ok := m[id]
	switch {
	case !ok:
		return homeworkflow.AssignmentView{}, shared.ErrNotFound
	case deleted:
		return homeworkflow.AssignmentView{}, fmt.Errorf("%w: %w", shared.ErrNotFound, assignment.ErrDeleted)
	}
	return homeworkflow.AssignmentView{ID: id}, nil
}

func TestMentionObjectsExist(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	sub, err := subject.New(subject.Spec{ID: "s1", Name: "Матан", Semester: libvalues.SemesterOf(now)}, now)
	if err != nil {
		t.Fatal(err)
	}
	newMat := func(id shared.MaterialID) *material.Material {
		m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeDOCX, Name: "Лаба.docx"}, now)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	removed := newMat("m2")
	if _, err := removed.Remove(now); err != nil {
		t.Fatal(err)
	}
	newLesson := func(id shared.LessonID) *lesson.Lesson {
		l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
			Title: "Матан", Start: now, End: now.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		}}, now)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	gone := newLesson("l2")
	if err := gone.MarkDeleted(now); err != nil {
		t.Fatal(err)
	}
	lib := mentionLibrary{subjects: []*subject.Subject{sub}, materials: map[shared.MaterialID]*material.Material{"m1": newMat("m1"), "m2": removed}}
	objs := mentionObjects{
		subjects: lib, materials: lib, lessons: mentionLessons{"l1": newLesson("l1"), "l2": gone},
		assignments: mentionAssignments{"a1": false, "a2": true},
	}

	tests := []struct {
		kind values.IDKind
		id   string
		want bool
	}{
		{values.KindSubject, "s1", true},
		{values.KindSubject, "s9", false},
		{values.KindMaterial, "m1", true},
		{values.KindMaterial, "m2", false}, // removed
		{values.KindMaterial, "m9", false},
		{values.KindLesson, "l1", true},
		{values.KindLesson, "l2", false}, // deleted
		{values.KindLesson, "l9", false},
		{values.KindAssignment, "a1", true},
		{values.KindAssignment, "a2", false}, // deleted
		{values.KindAssignment, "a9", false},
		{values.KindUpload, "u1", false}, // not a mention kind
	}
	for _, tt := range tests {
		t.Run(string(tt.kind)+"/"+tt.id, func(t *testing.T) {
			ref, err := values.NewRef(tt.kind, tt.id)
			if err != nil {
				t.Fatal(err)
			}
			got, err := objs.Exists(t.Context(), ref)
			if err != nil || got != tt.want {
				t.Fatalf("Exists = %v, %v; want %v", got, err, tt.want)
			}
		})
	}

	// A failing store is an error, never «does not exist».
	boom := errors.New("db down")
	broken := mentionObjects{subjects: mentionLibrary{err: boom}, materials: mentionLibrary{err: boom}}
	for _, kind := range []values.IDKind{values.KindSubject, values.KindMaterial} {
		ref, _ := values.NewRef(kind, "x1")
		if _, err := broken.Exists(t.Context(), ref); !errors.Is(err, boom) {
			t.Errorf("%s with a failing store = %v", kind, err)
		}
	}
}

// The «@» search of the panel is wired to the local database: a subject and an assignment are found by name, and a
// deleted assignment is not.
func TestAgentMentionsWiring(t *testing.T) {
	ctx := context.Background()
	a, _ := newTestAppWith(t, t.TempDir())
	ui := startCore(t, a)
	panel := wails.NewAgentService(ui.Agent, slog.New(slog.DiscardHandler))

	sub, err := ui.Library.CreateSubject(ctx, library.NewSubject{Name: "Матанализ"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := a.core.proposals.CreateAssignment(ctx, homeworkflow.NewAssignment{
		ID: "mention-test-1", Title: "Матанализ: ряды", Subject: sub.ID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := panel.MentionCandidates(ctx, "МАТАН")
	if got.Problem != "" || len(got.Candidates) != 2 {
		t.Fatalf("MentionCandidates = %+v", got)
	}
	if _, err := a.core.proposals.DeleteAssignment(ctx, v.ID, v.Version); err != nil {
		t.Fatal(err)
	}
	got = panel.MentionCandidates(ctx, "матан")
	if got.Problem != "" || len(got.Candidates) != 1 || got.Candidates[0].Kind != "subject" || got.Candidates[0].ID != string(sub.ID()) {
		t.Fatalf("after the delete = %+v", got)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
