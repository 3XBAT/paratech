package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

const rawCause = `save C:\Users\student\db: database is locked`

func TestDeleteAndRestoreAssignment(t *testing.T) {
	e := newHWEnv()
	del := e.svc.DeleteAssignment(context.Background(), "a1", 4)
	if del.Problem != "" || del.Assignment == nil || del.Assignment.ID != "a1" || del.Assignment.Version != 5 {
		t.Fatalf("DeleteAssignment = %+v, want a1 at version 5", del)
	}

	res := e.svc.RestoreAssignment(context.Background(), "a1", del.Assignment.Version)
	if res.Problem != "" || res.Assignment == nil || res.Assignment.Version != 6 {
		t.Fatalf("RestoreAssignment = %+v, want version 6", res)
	}

	if !slices.Equal(e.proposals.versions, []int64{4, 5}) {
		t.Fatalf("versions = %v, want the ones the page sent", e.proposals.versions)
	}
	if got := e.calls.list(); !slices.Equal(got, []string{"DeleteAssignment", "RestoreAssignment"}) {
		t.Fatalf("calls = %v, want only the use cases (no journal)", got)
	}
}

// A second click carries the version the page showed before the delete: the use case refuses it and nothing is
// deleted twice; a delete at the deleted version is not_found (the list does not show it).
func TestDeleteAssignmentRepeated(t *testing.T) {
	e := newHWEnv()
	if res := e.svc.DeleteAssignment(context.Background(), "a1", 1); res.Problem != "" {
		t.Fatalf("first delete = %+v", res)
	}

	e.proposals.deleteErr = fmt.Errorf("homeworkflow: delete assignment a1: %w", shared.ErrVersionConflict)
	if res := e.svc.DeleteAssignment(context.Background(), "a1", 1); res.Problem != changeChanged || res.Assignment != nil {
		t.Fatalf("repeated delete at the old version = %+v, want changed", res)
	}

	e.proposals.deleteErr = fmt.Errorf("homeworkflow: delete assignment a1: %w", assignment.ErrDeleted)
	if res := e.svc.DeleteAssignment(context.Background(), "a1", 2); res.Problem != changeNotFound || res.Assignment != nil {
		t.Fatalf("delete of a deleted assignment = %+v, want not_found", res)
	}
}

func TestChangeAssignmentProblems(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"in use", assignment.ErrInUse, changeInUse},
		{"other version", shared.ErrVersionConflict, changeChanged},
		{"not deleted", assignment.ErrNotDeleted, changeChanged},
		{"already deleted", assignment.ErrDeleted, changeNotFound},
		{"missing", shared.ErrNotFound, changeNotFound},
		{"failure", errors.New(rawCause), createFailed},
	}
	calls := []struct {
		name string
		set  func(f *fakeHWProposals, err error)
		call func(s *HomeworkService) ChangeResult
	}{
		{"DeleteAssignment", func(f *fakeHWProposals, err error) { f.deleteErr = err },
			func(s *HomeworkService) ChangeResult { return s.DeleteAssignment(context.Background(), "a1", 3) }},
		{"RestoreAssignment", func(f *fakeHWProposals, err error) { f.restoreErr = err },
			func(s *HomeworkService) ChangeResult { return s.RestoreAssignment(context.Background(), "a1", 3) }},
	}
	for _, c := range calls {
		for _, tt := range tests {
			t.Run(c.name+" "+tt.name, func(t *testing.T) {
				e := newHWEnv()
				c.set(e.proposals, fmt.Errorf("homeworkflow: change a1: %w", tt.err))
				res := c.call(e.svc)
				if res.Problem != tt.want || res.Assignment != nil {
					t.Fatalf("result = %+v, want %q", res, tt.want)
				}
				checkProblemLog(t, e, tt.want)
			})
		}
	}
}

func TestChangeAssignmentInvalidID(t *testing.T) {
	for _, id := range []string{"", "a b", "x\n", strings.Repeat("x", 300)} {
		e := newHWEnv()
		if res := e.svc.DeleteAssignment(context.Background(), id, 1); res != (ChangeResult{Problem: changeNotFound}) {
			t.Errorf("DeleteAssignment(%q) = %+v", id, res)
		}
		if res := e.svc.RestoreAssignment(context.Background(), id, 1); res != (ChangeResult{Problem: changeNotFound}) {
			t.Errorf("RestoreAssignment(%q) = %+v", id, res)
		}
		if res := e.svc.EditAssignment(context.Background(), EditRequest{ID: id, Version: 1, Title: "T"}); res != (EditResult{Problem: changeNotFound}) {
			t.Errorf("EditAssignment(%q) = %+v", id, res)
		}
		if got := e.calls.list(); len(got) != 0 {
			t.Errorf("id %q reached the use case: %v", id, got)
		}
	}
}

func TestEditAssignment(t *testing.T) {
	e := newHWEnv()
	res := e.svc.EditAssignment(context.Background(), EditRequest{
		ID: "a1", Version: 7, Title: "Read chapter 4", Note: "p. 12", SubjectID: "s1", Date: "2026-10-30", Time: "09:00",
	})
	if res.Problem != "" || res.Field != "" || res.Assignment == nil {
		t.Fatalf("result = %+v, want the assignment", res)
	}
	if a := res.Assignment; a.Title != "Read chapter 4" || a.Description != "p. 12" || a.Version != 8 || !a.UserEdited {
		t.Fatalf("assignment = %+v", *a)
	}

	want := homeworkflow.AssignmentEdit{
		ID: "a1", ExpectedVersion: 7, Title: "Read chapter 4", Note: "p. 12", Subject: "s1", Date: "2026-10-30", Time: "09:00",
	}
	if len(e.proposals.edits) != 1 || e.proposals.edits[0] != want {
		t.Fatalf("edits = %+v, want %+v", e.proposals.edits, want)
	}
	if got := e.calls.list(); !slices.Equal(got, []string{"EditAssignment"}) {
		t.Fatalf("calls = %v, want only EditAssignment (no journal)", got)
	}
}

func TestEditAssignmentEmptyOptionsAreNoValue(t *testing.T) {
	e := newHWEnv()
	if res := e.svc.EditAssignment(context.Background(), EditRequest{ID: "a1", Version: 1, Title: "T"}); res.Problem != "" {
		t.Fatalf("result = %+v", res)
	}
	if in := e.proposals.edits[0]; in.Subject != "" || in.Note != "" || in.Date != "" || in.Time != "" {
		t.Fatalf("input = %+v, want no subject, note, date, time", in)
	}
}

func TestEditAssignmentProblems(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		want  string
		field string
	}{
		{"title", assignment.ErrInvalidTitle, createInvalid, fieldTitle},
		// Also an untouched note of the model with bidi controls: the domain refuses it as the note.
		{"note", assignment.ErrInvalidNote, createInvalid, fieldNote},
		{"due", assignment.ErrInvalidDue, createInvalid, fieldDue},
		{"time", assignment.ErrInvalidTime, createInvalid, fieldTime},
		{"other details", assignment.ErrInvalidDetails, createInvalid, ""},
		{"unknown subject", homeworkflow.ErrSubjectNotFound, createSubjectNotFound, ""},
		{"other version", shared.ErrVersionConflict, changeChanged, ""},
		{"deleted", assignment.ErrDeleted, changeDeleted, ""},
		{"deleted as not found", fmt.Errorf("%w: %w", shared.ErrNotFound, assignment.ErrDeleted), changeDeleted, ""},
		{"missing", shared.ErrNotFound, changeNotFound, ""},
		{"in use", assignment.ErrInUse, createFailed, ""}, // edit is never refused as in use
		{"failure", errors.New(rawCause), createFailed, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newHWEnv()
			e.proposals.editErr = fmt.Errorf("homeworkflow: edit assignment a1: %w", tt.err)
			res := e.svc.EditAssignment(context.Background(), EditRequest{ID: "a1", Version: 1, Title: "T", Note: "x\u202ey"})
			if res.Problem != tt.want || res.Field != tt.field || res.Assignment != nil {
				t.Fatalf("result = %+v, want problem %q field %q", res, tt.want, tt.field)
			}
			checkProblemLog(t, e, tt.want)
		})
	}
}

// checkProblemLog: a known problem is logged by its code only, never the error text; a failure with its cause.
func checkProblemLog(t *testing.T, e *hwEnv, problem string) {
	t.Helper()
	logs := e.logs.String()
	if problem == createFailed {
		if !strings.Contains(logs, "locked") && !strings.Contains(logs, assignment.ErrInUse.Error()) {
			t.Fatalf("the cause of a failure is not logged: %q", logs)
		}
		return
	}
	if !strings.Contains(logs, "problem="+problem) || strings.Contains(logs, "homeworkflow:") {
		t.Fatalf("log = %q, want only the code %q", logs, problem)
	}
}

func TestAssignmentItemVersionAndLessonSubject(t *testing.T) {
	tests := []struct {
		source values.NameSource
		want   bool
	}{
		{values.SourceSchedule, true},
		{values.SourceTranscript, true},
		{values.SourceUser, false},
		{values.SourceNone, false},
	}
	for _, tt := range tests {
		got := assignmentItem(homeworkflow.AssignmentView{ID: "a1", LessonID: "l1", Version: 9, Subject: "Физика", SubjectSource: tt.source})
		if got.SubjectFromLesson != tt.want || got.Version != 9 {
			t.Errorf("source %q: item = %+v, want subjectFromLesson %v, version 9", tt.source, got, tt.want)
		}
	}
}

// A send of an assignment deleted after the dialog opened has its own text (TASK-229 reason assignment_deleted).
func TestConfirmSendOfDeletedAssignment(t *testing.T) {
	e := newHWEnv()
	e.proposals.confirmErr = fmt.Errorf("execute: %w: %s", proposal.ErrStale, proposal.ReasonAssignmentDeleted)
	e.proposals.proposal.Status, e.proposals.proposal.Reason = proposal.StatusStale, proposal.ReasonAssignmentDeleted
	if err := e.svc.ConfirmSend(context.Background(), "p1", "ДЗ", "txt", "", ""); !errors.Is(err, ErrSendAssignmentDeleted) {
		t.Fatalf("ConfirmSend = %v, want ErrSendAssignmentDeleted", err)
	}

	e = newHWEnv()
	e.proposals.confirmErr = fmt.Errorf("execute: %w: %s", proposal.ErrStale, proposal.ReasonSubjectChanged)
	e.proposals.proposal.Status, e.proposals.proposal.Reason = proposal.StatusStale, proposal.ReasonSubjectChanged
	if err := e.svc.ConfirmSend(context.Background(), "p1", "ДЗ", "txt", "", ""); !errors.Is(err, ErrSendStale) {
		t.Fatalf("ConfirmSend of another stale reason = %v, want ErrSendStale", err)
	}
}

// The page reads these names (frontend/src/bindings/homework.ts).
func TestChangeJSONNames(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{ChangeResult{}, `{"problem":"","assignment":null}`},
		{EditResult{}, `{"problem":"","field":"","assignment":null}`},
		{EditRequest{}, `{"id":"","version":0,"title":"","note":"","subjectId":"","date":"","time":""}`},
	}
	for _, tt := range tests {
		raw, err := json.Marshal(tt.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tt.want {
			t.Errorf("%T JSON = %s\nwant %s", tt.v, raw, tt.want)
		}
	}
}
