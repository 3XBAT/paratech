package wails

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const opCreateAssignment = "ui: create assignment"

// Problems of CreateAssignment as the page reads them. The page owns the Russian texts.
const (
	createInvalid         = "invalid" // see CreateResult.Field
	createSubjectNotFound = "subject_not_found"
	createLessonNotFound  = "lesson_not_found"
	createFailed          = "failed"
)

// Fields of an invalid CreateAssignment request.
const (
	fieldRequestID = "requestId"
	fieldTitle     = "title"
	fieldNote      = "note"
	fieldDue       = "due"
	fieldTime      = "time"
)

const (
	maxRequestIDLen = 64
	requestIDPrefix = "ui:" // TASK-217 hashes the agent's calls under another prefix, so the ids never collide
)

// CreateRequest is the add-assignment form. Empty SubjectID, LessonID, Date and Time mean no value; Date is
// YYYY-MM-DD and Time HH:MM (no time: the end of that day). RequestID is the form's token: sending the same form
// again stores one assignment.
type CreateRequest struct {
	RequestID string `json:"requestId"`
	Title     string `json:"title"`
	SubjectID string `json:"subjectId"`
	LessonID  string `json:"lessonId"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Note      string `json:"note"`
}

// CreateResult answers CreateAssignment: Problem "" with Assignment set, or a problem code with Assignment null.
// Field names the input of an invalid request ("requestId", "title", "note", "due" or "time"; "" when unknown).
type CreateResult struct {
	Problem    string          `json:"problem"`
	Field      string          `json:"field"`
	Assignment *AssignmentItem `json:"assignment"`
}

// CreateAssignment adds an assignment or a reminder the student typed in (ADR-0029). The click is the student's own
// action: nothing is written to the agent journal. The id is derived from RequestID, so sending the same form twice
// gives one assignment. Errors are codes only; the page shows the texts and the causes go to the log.
func (s *HomeworkService) CreateAssignment(ctx context.Context, req CreateRequest) CreateResult {
	if !validRequestID(req.RequestID) {
		return CreateResult{Problem: createInvalid, Field: fieldRequestID}
	}

	v, err := s.proposals.CreateAssignment(ctx, homeworkflow.NewAssignment{
		ID:      createdID(req.RequestID),
		Title:   req.Title,
		Note:    req.Note,
		Subject: shared.SubjectID(req.SubjectID),
		Lesson:  shared.LessonID(req.LessonID),
		Date:    req.Date,
		Time:    req.Time,
	})
	if err != nil {
		return s.createProblem(err)
	}

	item := assignmentItem(v)
	return CreateResult{Assignment: &item}
}

// createProblem maps a use case error to a code of the page.
func (s *HomeworkService) createProblem(err error) CreateResult {
	switch {
	case errors.Is(err, assignment.ErrInvalidTitle):
		return CreateResult{Problem: createInvalid, Field: fieldTitle}
	case errors.Is(err, assignment.ErrInvalidNote):
		return CreateResult{Problem: createInvalid, Field: fieldNote}
	case errors.Is(err, assignment.ErrInvalidDue):
		return CreateResult{Problem: createInvalid, Field: fieldDue}
	case errors.Is(err, assignment.ErrInvalidTime):
		return CreateResult{Problem: createInvalid, Field: fieldTime}
	case errors.Is(err, assignment.ErrInvalidDetails):
		return CreateResult{Problem: createInvalid}
	case errors.Is(err, homeworkflow.ErrSubjectNotFound):
		s.log.Info(opCreateAssignment, "result", err)
		return CreateResult{Problem: createSubjectNotFound}
	case errors.Is(err, shared.ErrNotFound): // the use case wraps it only for an unknown lesson
		s.log.Info(opCreateAssignment, "result", "lesson not found")
		return CreateResult{Problem: createLessonNotFound}
	}
	// Includes shared.ErrDuplicate: the same form sent again with other content.
	s.log.Error(opCreateAssignment, "err", err)
	return CreateResult{Problem: createFailed}
}

// validRequestID accepts 1..64 characters of [A-Za-z0-9-] (a UUID fits).
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// createdID is the assignment id of a form token: "ui-" and the first 16 bytes of SHA-256("ui:<token>") in hex.
func createdID(requestID string) shared.AssignmentID {
	sum := sha256.Sum256([]byte(requestIDPrefix + requestID))
	return shared.AssignmentID("ui-" + hex.EncodeToString(sum[:16]))
}
