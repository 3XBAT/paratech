package wails

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

func TestCreateAssignment(t *testing.T) {
	e := newHWEnv()
	res := e.svc.CreateAssignment(context.Background(), CreateRequest{
		RequestID: "3f2b-77", Title: "Read chapter 3", SubjectID: "s1", LessonID: "l1", Date: "2026-10-30", Time: "09:00", Note: "p. 10",
	})
	if res.Problem != "" || res.Field != "" || res.Assignment == nil {
		t.Fatalf("result = %+v, want the assignment", res)
	}
	if res.Assignment.Title != "Read chapter 3" || res.Assignment.Description != "p. 10" {
		t.Fatalf("assignment = %+v", *res.Assignment)
	}

	if len(e.proposals.createCalls) != 1 {
		t.Fatalf("calls = %d, want 1", len(e.proposals.createCalls))
	}
	in := e.proposals.createCalls[0]
	if in.Title != "Read chapter 3" || in.Note != "p. 10" || in.Subject != "s1" || in.Lesson != "l1" || in.Date != "2026-10-30" || in.Time != "09:00" {
		t.Fatalf("input = %+v", in)
	}
	if err := in.ID.Validate(); err != nil || string(in.ID) != string(res.Assignment.ID) {
		t.Fatalf("id = %q (%v), assignment id %q", in.ID, err, res.Assignment.ID)
	}
	if got := e.calls.list(); len(got) != 1 {
		t.Fatalf("calls = %v, want only CreateAssignment (no journal, no other use case)", got)
	}
}

func TestCreateAssignmentEmptyOptionsAreNoValue(t *testing.T) {
	e := newHWEnv()
	if res := e.svc.CreateAssignment(context.Background(), CreateRequest{RequestID: "r1", Title: "T"}); res.Problem != "" {
		t.Fatalf("result = %+v", res)
	}
	in := e.proposals.createCalls[0]
	if in.Subject != "" || in.Lesson != "" || in.Date != "" || in.Time != "" {
		t.Fatalf("input = %+v, want no subject, lesson, date, time", in)
	}
}

// The same form sent twice is one assignment; another token is another one.
func TestCreateAssignmentSameRequestIDIsOneAssignment(t *testing.T) {
	e := newHWEnv()
	req := CreateRequest{RequestID: "form-1", Title: "T"}
	a := e.svc.CreateAssignment(context.Background(), req)
	b := e.svc.CreateAssignment(context.Background(), req)
	if a.Assignment == nil || b.Assignment == nil || a.Assignment.ID != b.Assignment.ID {
		t.Fatalf("ids differ: %+v %+v", a, b)
	}
	if len(e.proposals.created) != 1 {
		t.Fatalf("stored = %d, want 1", len(e.proposals.created))
	}

	req.RequestID = "form-2"
	c := e.svc.CreateAssignment(context.Background(), req)
	if c.Assignment == nil || c.Assignment.ID == a.Assignment.ID || len(e.proposals.created) != 2 {
		t.Fatalf("another token must give another assignment: %+v", c)
	}
}

func TestCreatedIDIsStableAndValid(t *testing.T) {
	long := strings.Repeat("a", maxRequestIDLen)
	for _, tok := range []string{"a", "A-9", long} {
		id := createdID(tok)
		if err := id.Validate(); err != nil {
			t.Errorf("createdID(%q) = %q: %v", tok, id, err)
		}
		if id != createdID(tok) {
			t.Errorf("createdID(%q) is not stable", tok)
		}
	}
	if createdID("a") == createdID("b") {
		t.Error("different tokens share an id")
	}
}

func TestCreateAssignmentBadRequestID(t *testing.T) {
	for _, id := range []string{"", "a b", "a\n", "a_b", "a/b", "ж", "a.b", strings.Repeat("a", maxRequestIDLen+1)} {
		e := newHWEnv()
		res := e.svc.CreateAssignment(context.Background(), CreateRequest{RequestID: id, Title: "T"})
		if res.Problem != createInvalid || res.Field != fieldRequestID || res.Assignment != nil {
			t.Errorf("RequestID %q: result = %+v", id, res)
		}
		if got := e.calls.list(); len(got) != 0 {
			t.Errorf("RequestID %q reached the use case: %v", id, got)
		}
	}
	if res := newHWEnv().svc.CreateAssignment(context.Background(), CreateRequest{RequestID: strings.Repeat("a", maxRequestIDLen), Title: "T"}); res.Problem != "" {
		t.Errorf("a 64-character token is refused: %+v", res)
	}
}

func TestCreateAssignmentProblems(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  string
		field string
		log   string // must be logged; "" none
	}{
		{"title", fmt.Errorf("create: %w", assignment.ErrInvalidTitle), createInvalid, fieldTitle, ""},
		{"note", fmt.Errorf("create: %w", assignment.ErrInvalidNote), createInvalid, fieldNote, ""},
		{"due", fmt.Errorf("create: %w", assignment.ErrInvalidDue), createInvalid, fieldDue, ""},
		{"time", fmt.Errorf("create: %w", assignment.ErrInvalidTime), createInvalid, fieldTime, ""},
		{"other details", fmt.Errorf("create: %w", assignment.ErrInvalidDetails), createInvalid, "", ""},
		{"unknown subject", fmt.Errorf("create: %w", homeworkflow.ErrSubjectNotFound), createSubjectNotFound, "", ""},
		{"unknown lesson", fmt.Errorf("create: lesson: %w", shared.ErrNotFound), createLessonNotFound, "", ""},
		{"duplicate", fmt.Errorf("create: %w", shared.ErrDuplicate), createFailed, "", "create:"},
		{"failure", errors.New(`save C:\Users\student\db: database is locked`), createFailed, "", "locked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHWEnv()
			e.proposals.createErr = tt.err
			res := e.svc.CreateAssignment(context.Background(), CreateRequest{RequestID: "r1", Title: "T"})
			if res.Problem != tt.want || res.Field != tt.field || res.Assignment != nil {
				t.Fatalf("result = %+v, want problem %q field %q", res, tt.want, tt.field)
			}
			if tt.log != "" && !strings.Contains(e.logs.String(), tt.log) {
				t.Fatalf("the cause is not logged: %q", e.logs.String())
			}
		})
	}
}
