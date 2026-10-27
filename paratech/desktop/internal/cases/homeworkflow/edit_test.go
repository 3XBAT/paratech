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

// Editing an assignment the student added (ADR-0029, addendum 2026-10-27): every field, the subject as the
// student's correction, the deadline in the zone of the clock.
func TestEditAssignmentByStudent(t *testing.T) {
	cases := []struct {
		name        string
		edit        AssignmentEdit
		wantSubject string
		wantSource  values.NameSource
		wantDue     time.Time
	}{
		{
			name: "subject and a date: 23:59", edit: AssignmentEdit{Title: "Решить 6", Note: "Стр. 12", Subject: "s1", Date: "2026-10-30"},
			wantSubject: "Матанализ", wantSource: values.SourceUser, wantDue: time.Date(2026, 10, 30, 23, 59, 0, 0, msk),
		},
		{
			name: "a time of day", edit: AssignmentEdit{Title: "Решить 6", Subject: "s1", Date: "2026-10-15", Time: "09:00"},
			wantSubject: "Матанализ", wantSource: values.SourceUser, wantDue: time.Date(2026, 10, 15, 9, 0, 0, 0, msk),
		},
		{name: "no subject and no deadline", edit: AssignmentEdit{Title: "Решить 6"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCreateEnv(t, testSubjects)
			ctx := context.Background()
			created, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Решить 5", Note: "old", Subject: "s1", Date: "2026-10-20"})
			if err != nil {
				t.Fatal(err)
			}
			e.clock.Advance(time.Minute)

			in := c.edit
			in.ID, in.ExpectedVersion = created.ID, created.Version
			v, err := e.svc.EditAssignment(ctx, in)
			if err != nil {
				t.Fatalf("EditAssignment: %v", err)
			}
			if v.Title != in.Title || v.Description != in.Note || v.Subject != c.wantSubject || v.SubjectSource != c.wantSource ||
				!v.Due.Equal(c.wantDue) || !v.UserEdited || v.Version != created.Version+1 || !v.UpdatedAt.Equal(ppT0.Add(time.Minute)) {
				t.Fatalf("view = %+v", v)
			}
			if st := e.assignments[created.ID]; st.UserSubject != c.wantSubject || st.SourceIndex != -1 {
				t.Fatalf("stored = %+v", st)
			}
			if got, err := e.svc.Assignment(ctx, created.ID); err != nil || got != v {
				t.Fatalf("Assignment = %+v, %v; want %+v", got, err, v)
			}
		})
	}
}

// An extracted assignment takes a subject and a deadline outside the window of a student one; it stays extracted.
func TestEditAssignmentExtracted(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	e.llm.Answer(values.OperationExtractHomework, extractAnswer)
	if err := e.handle(e.snapshot("j1", ExtractHomeworkJobType, "t1")); err != nil {
		t.Fatal(err)
	}
	st := e.assignmentsOf("t1")[0]
	ctx := context.Background()

	v, err := e.svc.EditAssignment(ctx, AssignmentEdit{
		ID: st.ID, ExpectedVersion: st.Version, Title: "Глава 4", Note: "a\tb", Subject: "s1", Date: "2029-01-01", Time: "10:30",
	})
	if err != nil {
		t.Fatalf("EditAssignment: %v", err)
	}
	if v.Subject != "Матанализ" || v.SubjectSource != values.SourceUser || !v.Due.Equal(time.Date(2029, 1, 1, 10, 30, 0, 0, msk)) ||
		!v.UserEdited || v.Description != "a\tb" {
		t.Fatalf("view = %+v", v)
	}
	if got := e.assignmentsOf("t1")[0]; got.SourceIndex != st.SourceIndex || got.Transcript != "t1" || !got.UserEdited {
		t.Fatalf("stored = %+v", got)
	}
}

func TestEditAssignmentUseCaseRefusals(t *testing.T) {
	cases := []struct {
		name     string
		subjects SubjectNames
		edit     func(v AssignmentView) AssignmentEdit
		want     error
	}{
		{"unknown subject", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Subject: "s9"}
		}, ErrSubjectNotFound},
		{"malformed subject", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Subject: "s 9"}
		}, ErrSubjectNotFound},
		{"subjects not wired", nil, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Subject: "s1"}
		}, ErrInvalidConfig},
		{"other version", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version + 1, Title: "x"}
		}, shared.ErrVersionConflict},
		{"due outside the window", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Date: "2029-01-01"}
		}, assignment.ErrInvalidDue},
		{"due long ago", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Date: "2026-09-01"}
		}, assignment.ErrInvalidDue},
		{"bad time", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Date: "2026-10-20", Time: "9:00"}
		}, assignment.ErrInvalidTime},
		{"bidi in the title", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "a\u2066b"}
		}, assignment.ErrInvalidTitle},
		{"note over the student limit", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: v.ID, ExpectedVersion: v.Version, Title: "x", Note: strings.Repeat("я", assignment.MaxNoteRunes+1)}
		}, assignment.ErrInvalidNote},
		{"unknown assignment", testSubjects, func(v AssignmentView) AssignmentEdit {
			return AssignmentEdit{ID: "a-missing", ExpectedVersion: v.Version, Title: "x"}
		}, shared.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newCreateEnv(t, c.subjects)
			ctx := context.Background()
			created, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Решить 5"})
			if err != nil {
				t.Fatal(err)
			}
			before := e.assignments[created.ID]

			_, err = e.svc.EditAssignment(ctx, c.edit(created))
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if errors.Is(err, ErrSubjectNotFound) && errors.Is(err, shared.ErrNotFound) {
				t.Fatalf("an unknown subject reads as an unknown assignment: %v", err)
			}
			if e.assignments[created.ID] != before {
				t.Fatal("a refused edit changed the assignment")
			}
		})
	}
}

func TestEditAssignmentOfADeletedOne(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	created, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Решить 5"})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := e.svc.DeleteAssignment(ctx, created.ID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	before := e.assignments[created.ID]

	if _, err := e.svc.EditAssignment(ctx, AssignmentEdit{ID: created.ID, ExpectedVersion: deleted.Version, Title: "x"}); !errors.Is(err, assignment.ErrDeleted) {
		t.Fatalf("EditAssignment: err = %v, want ErrDeleted", err)
	}
	if _, err := e.svc.SetAssignmentDue(ctx, created.ID, deleted.Version, ppT0); !errors.Is(err, assignment.ErrDeleted) {
		t.Fatalf("SetAssignmentDue: err = %v, want ErrDeleted", err)
	}
	if e.assignments[created.ID] != before {
		t.Fatal("the deleted assignment changed")
	}
}

// SetAssignmentDue is the agent's edit_due as it was before EditAssignment grew: only the deadline, to the exact
// instant, with the rules of any edit (no window of a student assignment), and the version is checked.
func TestSetAssignmentDue(t *testing.T) {
	e := newCreateEnv(t, testSubjects)
	ctx := context.Background()
	created, err := e.svc.CreateAssignment(ctx, NewAssignment{Title: "Решить 5", Note: "Стр. 10", Subject: "s1", Date: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}

	due := time.Date(2029, 3, 1, 17, 45, 30, 0, msk)
	v, err := e.svc.SetAssignmentDue(ctx, created.ID, created.Version, due)
	if err != nil {
		t.Fatalf("SetAssignmentDue: %v", err)
	}
	if !v.Due.Equal(due) || v.Title != "Решить 5" || v.Description != "Стр. 10" || v.Subject != "Матанализ" ||
		v.Version != created.Version+1 || !v.UserEdited {
		t.Fatalf("view = %+v", v)
	}

	if _, err := e.svc.SetAssignmentDue(ctx, created.ID, created.Version, ppT0); !errors.Is(err, shared.ErrVersionConflict) {
		t.Fatalf("other version: err = %v", err)
	}
	v, err = e.svc.SetAssignmentDue(ctx, created.ID, v.Version, time.Time{})
	if err != nil || !v.Due.IsZero() {
		t.Fatalf("no deadline = %+v, %v", v, err)
	}
	if _, err := e.svc.SetAssignmentDue(ctx, "a-missing", 1, ppT0); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing: err = %v", err)
	}
}
