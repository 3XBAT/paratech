package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// An assignment the student adds with a library subject (TASK-215, ADR-0029) shows with that subject in the list,
// on «Сегодня» (the Deadlines the app builds for todayfeed) and resolves back to the library subject; an unknown
// subject is refused, and the Undo removes it.
func TestStudentAssignmentIsVisibleWithItsSubject(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, filepath.Join(t.TempDir(), "data"))
	ui, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := ui.Library.CreateSubject(ctx, library.NewSubject{Name: "Матанализ"})
	if err != nil {
		t.Fatal(err)
	}
	homework := a.core.proposals
	date := a.env.local.Now().AddDate(0, 0, 2).Format(time.DateOnly)

	v, err := homework.CreateAssignment(ctx, homeworkflow.NewAssignment{
		ID: "ui-test-1", Title: "Решить номер 5", Subject: sub.ID(), Date: date, Time: "09:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Subject != "Матанализ" || v.SubjectSource != values.SourceUser || v.Due.IsZero() {
		t.Fatalf("view = %+v", v)
	}

	list, err := homework.ListAssignments(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "ui-test-1" || list[0].Subject != "Матанализ" {
		t.Fatalf("ListAssignments = %+v, %v", list, err)
	}
	deadlines, err := todayAssignments{homework}.Deadlines(ctx)
	if err != nil || len(deadlines) != 1 || deadlines[0].Subject != "Матанализ" || !deadlines[0].Due.Equal(v.Due) {
		t.Fatalf("Deadlines = %+v, %v", deadlines, err)
	}
	lib, ok := ui.Library.(*library.Service)
	if !ok {
		t.Fatalf("the library is %T", ui.Library)
	}
	matches, err := lib.ResolveSubjects(ctx, []string{deadlines[0].Subject})
	if m, ok := matches["Матанализ"]; err != nil || !ok || m.ID != sub.ID() {
		t.Fatalf("ResolveSubjects = %+v, %v", matches, err)
	}

	if _, err := homework.CreateAssignment(ctx, homeworkflow.NewAssignment{Title: "x", Subject: "no-such"}); !errors.Is(err, homeworkflow.ErrSubjectNotFound) {
		t.Fatalf("unknown subject: err = %v", err)
	}

	if err := homework.RemoveCreatedAssignment(ctx, v.ID, v.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := homework.Assignment(ctx, v.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("after the Undo: err = %v", err)
	}
}
