package homeworkflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// editEnv is the Service with the assignment store of the post-processing fakes, after one extraction of t1.
type editEnv struct {
	*ppEnv
	svc *Service
}

func newEditEnv(t *testing.T) *editEnv {
	t.Helper()
	pe := newPPEnv(t)
	pe.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := pe.handle(pe.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	svc, err := New(Deps{
		Proposals: proposalRepo{e.store}, Assignments: ppAssignments{pe.ppStore},
		AssignmentList: ppAssignmentList{pe.ppStore}, Artifacts: e.files,
		Recipients: recipients{e.store}, Mailer: e.mailer, Exporter: e.exporter, Calendar: e.calendar,
		Recordings: ppTranscriptRecordings{pe.ppStore}, Lessons: ppLessons{pe.ppStore},
		Clock: pe.clock, IDs: ids{e.store}, Tx: tx{e.store},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &editEnv{ppEnv: pe, svc: svc}
}

func (e *editEnv) first() assignment.State { return e.assignmentsOf("t1")[0] }

func TestEditAssignment(t *testing.T) {
	e := newEditEnv(t)
	id := e.first().ID
	due := time.Date(2026, 10, 19, 9, 0, 0, 0, msk)
	e.clock.Advance(time.Hour)

	view, err := e.svc.EditAssignment(context.Background(), AssignmentEdit{
		ID: id, Title: "  Read chapter 4 and 5  ", Description: "With notes", Due: due,
	})
	if err != nil {
		t.Fatalf("EditAssignment: %v", err)
	}
	if view.Title != "Read chapter 4 and 5" || view.Description != "With notes" || !view.Due.Equal(due) || !view.UserEdited ||
		view.Version != 2 || view.LessonID != "l1" || !view.UpdatedAt.Equal(ppT0.Add(time.Hour)) {
		t.Fatalf("view = %+v", view)
	}
	stored := e.first()
	if !stored.UserEdited || stored.Title != "Read chapter 4 and 5" || !stored.Due.Equal(due) {
		t.Fatalf("stored = %+v", stored)
	}

	got, err := e.svc.Assignment(context.Background(), id)
	if err != nil || got != view {
		t.Fatalf("Assignment = %+v, %v; want %+v", got, err, view)
	}

	// Removing the deadline is an edit too.
	view, err = e.svc.EditAssignment(context.Background(), AssignmentEdit{ID: id, Title: "Read chapter 4 and 5"})
	if err != nil || !view.Due.IsZero() {
		t.Fatalf("EditAssignment without a deadline = %+v, %v", view, err)
	}
}

func TestEditAssignmentRefusals(t *testing.T) {
	tests := []struct {
		name string
		edit func(id shared.AssignmentID) AssignmentEdit
		want error
	}{
		{"empty title", func(id shared.AssignmentID) AssignmentEdit { return AssignmentEdit{ID: id, Title: " "} }, assignment.ErrInvalidDetails},
		{"multi-line title", func(id shared.AssignmentID) AssignmentEdit { return AssignmentEdit{ID: id, Title: "a\nb"} }, assignment.ErrInvalidDetails},
		{"description too long", func(id shared.AssignmentID) AssignmentEdit {
			return AssignmentEdit{ID: id, Title: "x", Description: strings.Repeat("a", assignment.MaxDescriptionRunes+1)}
		}, assignment.ErrInvalidDetails},
		{"unknown assignment", func(shared.AssignmentID) AssignmentEdit { return AssignmentEdit{ID: "nope", Title: "x"} }, shared.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEditEnv(t)
			before := e.first()
			if _, err := e.svc.EditAssignment(context.Background(), tc.edit(before.ID)); !errors.Is(err, tc.want) {
				t.Fatalf("EditAssignment = %v, want %v", err, tc.want)
			}
			if e.first() != before {
				t.Fatal("a refused edit changed the assignment")
			}
		})
	}
}

// AC-3 of TASK-042 through the use cases: an edit made in the UI survives a reprocessing job.
func TestEditSurvivesReprocessing(t *testing.T) {
	e := newEditEnv(t)
	id := e.first().ID
	if _, err := e.svc.EditAssignment(context.Background(), AssignmentEdit{ID: id, Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	e.llm.Answer(values.OperationExtractHomework, `{"assignments":[{"title":"Theirs","description":"","due":null}]}`)
	if err := e.handle(e.snapshot("j2", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	if got := e.first(); got.Title != "Mine" || !got.UserEdited || !got.Due.IsZero() {
		t.Fatalf("after reprocessing: %+v", got)
	}
}
