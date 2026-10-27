package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const (
	opDeleteAssignment  = "ui: delete assignment"
	opRestoreAssignment = "ui: restore assignment"
	opEditAssignment    = "ui: edit assignment"
)

// Problems of DeleteAssignment, RestoreAssignment and EditAssignment as the page reads them, besides createInvalid,
// createSubjectNotFound and createFailed. The page owns the Russian texts.
const (
	changeInUse    = "in_use"    // something in progress uses the assignment (an unresolved email, a running draft)
	changeChanged  = "changed"   // the assignment is not at the version the page showed: reload
	changeDeleted  = "deleted"   // edit of a deleted assignment
	changeNotFound = "not_found" // no such assignment (also an invalid id, or a delete of a deleted one)
)

// ChangeResult answers DeleteAssignment and RestoreAssignment: Problem "" with Assignment set (the deleted or the
// restored assignment, with its new version for «Вернуть»), or a problem code with Assignment null.
type ChangeResult struct {
	Problem    string          `json:"problem"`
	Assignment *AssignmentItem `json:"assignment"`
}

// EditRequest is the edit form of an assignment of any origin. Version is AssignmentItem.Version the form showed.
// Empty SubjectID, Date and Time mean no value; Date is YYYY-MM-DD and Time HH:MM (no time: the end of that day).
type EditRequest struct {
	ID        string `json:"id"`
	Version   int64  `json:"version"`
	Title     string `json:"title"`
	Note      string `json:"note"`
	SubjectID string `json:"subjectId"`
	Date      string `json:"date"`
	Time      string `json:"time"`
}

// EditResult answers EditAssignment: Problem "" with Assignment set, or a problem code with Assignment null. Field
// names the input of an invalid request ("title", "note", "due" or "time"; "" when unknown).
type EditResult struct {
	Problem    string          `json:"problem"`
	Field      string          `json:"field"`
	Assignment *AssignmentItem `json:"assignment"`
}

// DeleteAssignment is «Удалить» (ADR-0029, addendum 2026-10-27): a soft delete of the assignment at version. The
// click is the student's own action: nothing is written to the agent journal. A repeated click carries the old
// version and gets "changed"; nothing is deleted twice.
func (s *HomeworkService) DeleteAssignment(ctx context.Context, id string, version int64) ChangeResult {
	return s.change(ctx, opDeleteAssignment, id, version, s.proposals.DeleteAssignment)
}

// RestoreAssignment is «Вернуть»: the deleted assignment at version shows again.
func (s *HomeworkService) RestoreAssignment(ctx context.Context, id string, version int64) ChangeResult {
	return s.change(ctx, opRestoreAssignment, id, version, s.proposals.RestoreAssignment)
}

func (s *HomeworkService) change(ctx context.Context, op, id string, version int64,
	apply func(context.Context, shared.AssignmentID, int64) (homeworkflow.AssignmentView, error)) ChangeResult {
	aid, err := assignmentID(id)
	if err != nil {
		return ChangeResult{Problem: changeNotFound}
	}
	v, err := apply(ctx, aid, version)
	if err != nil {
		return ChangeResult{Problem: s.changeProblem(op, err)}
	}
	item := assignmentItem(v)
	return ChangeResult{Assignment: &item}
}

// changeProblem maps an error of DeleteAssignment or RestoreAssignment to a code of the page.
func (s *HomeworkService) changeProblem(op string, err error) string {
	code := createFailed
	switch {
	case errors.Is(err, assignment.ErrInUse):
		code = changeInUse
	case errors.Is(err, shared.ErrVersionConflict), errors.Is(err, assignment.ErrNotDeleted):
		code = changeChanged
	case errors.Is(err, assignment.ErrDeleted), errors.Is(err, shared.ErrNotFound):
		code = changeNotFound // already deleted: the list does not show it any more
	}
	s.logProblem(op, code, err)
	return code
}

// EditAssignment stores the student's title, note, deadline and subject of an assignment of any origin (ADR-0029,
// addendum 2026-10-27). The click is the student's own action: nothing is written to the agent journal. An untouched
// note of the model that the rules refuse (bidi controls) is "invalid" with Field "note": the page shows it there.
func (s *HomeworkService) EditAssignment(ctx context.Context, req EditRequest) EditResult {
	aid, err := assignmentID(req.ID)
	if err != nil {
		return EditResult{Problem: changeNotFound}
	}
	v, err := s.proposals.EditAssignment(ctx, homeworkflow.AssignmentEdit{
		ID:              aid,
		ExpectedVersion: req.Version,
		Title:           req.Title,
		Note:            req.Note,
		Subject:         shared.SubjectID(req.SubjectID),
		Date:            req.Date,
		Time:            req.Time,
	})
	if err != nil {
		return s.editProblem(err)
	}
	item := assignmentItem(v)
	return EditResult{Assignment: &item}
}

// editProblem maps an error of EditAssignment to a code of the page.
func (s *HomeworkService) editProblem(err error) EditResult {
	var out EditResult
	switch {
	case errors.Is(err, assignment.ErrInvalidTitle):
		out = EditResult{Problem: createInvalid, Field: fieldTitle}
	case errors.Is(err, assignment.ErrInvalidNote):
		out = EditResult{Problem: createInvalid, Field: fieldNote}
	case errors.Is(err, assignment.ErrInvalidDue):
		out = EditResult{Problem: createInvalid, Field: fieldDue}
	case errors.Is(err, assignment.ErrInvalidTime):
		out = EditResult{Problem: createInvalid, Field: fieldTime}
	case errors.Is(err, assignment.ErrInvalidDetails):
		out = EditResult{Problem: createInvalid}
	case errors.Is(err, homeworkflow.ErrSubjectNotFound):
		out = EditResult{Problem: createSubjectNotFound}
	case errors.Is(err, shared.ErrVersionConflict):
		out = EditResult{Problem: changeChanged}
	case errors.Is(err, assignment.ErrDeleted): // before ErrNotFound: a reader may wrap both
		out = EditResult{Problem: changeDeleted}
	case errors.Is(err, shared.ErrNotFound):
		out = EditResult{Problem: changeNotFound}
	default:
		out = EditResult{Problem: createFailed}
	}
	s.logProblem(opEditAssignment, out.Problem, err)
	return out
}

// logProblem logs a known problem by its code only (the errors name ids and hold no text worth keeping); an
// unexpected failure is logged with its cause, as by the other methods of the page.
func (s *HomeworkService) logProblem(op, code string, err error) {
	if code == createFailed {
		s.log.Error(op, "err", err)
		return
	}
	s.log.Info(op, "problem", code)
}
