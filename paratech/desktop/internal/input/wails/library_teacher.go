package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opLibrarySetTeacher   = "ui: set subject teacher"
	opLibraryClearTeacher = "ui: clear subject teacher"
)

// Problems of SetSubjectTeacher and ClearSubjectTeacher as the page reads them. The page owns the Russian texts.
const (
	teacherInvalid  = "invalid" // see LibraryTeacherResult.Field
	teacherChanged  = "changed"
	teacherNotFound = "not_found"
	teacherFailed   = "failed"
)

// Fields of an invalid teacher.
const (
	fieldTeacherName  = "name"
	fieldTeacherEmail = "email"
	fieldTeacherRole  = "role"
)

// LibraryTeacher is a teacher of a subject (ADR-0030). Both texts are personal data typed by the student: plain
// text only, never logged.
type LibraryTeacher struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// LibraryTeachers are the teachers of a subject by role (the role names of subject.TeacherRole); null for a role
// without one. «Как у лектора» / «Как у семинариста» copy a role in the page, there is no call for it.
type LibraryTeachers struct {
	Lecture  *LibraryTeacher `json:"lecture"`
	Practice *LibraryTeacher `json:"practice"`
	Lab      *LibraryTeacher `json:"lab"`
}

// LibraryTeacherResult answers SetSubjectTeacher and ClearSubjectTeacher: Problem "" with Subject set, or a problem
// code ("invalid", "changed", "not_found", "failed") with Subject null. Field names the input of "invalid" ("name",
// "email" or "role"). "changed": the subject is no longer at the version the page saw; the page reloads it.
type LibraryTeacherResult struct {
	Problem string          `json:"problem"`
	Field   string          `json:"field"`
	Subject *LibrarySubject `json:"subject"`
}

// SetSubjectTeacher sets the teacher of role ("lecture", "practice" or "lab") of a subject while it is still at
// version, the one the page shows. It is the student's action only; nothing is sent. The log gets only a code, never
// the name or the address.
func (s *LibraryService) SetSubjectTeacher(ctx context.Context, subjectID string, version int64, role, name, email string) LibraryTeacherResult {
	sub, err := s.uc.SetTeacher(ctx, shared.SubjectID(subjectID), subject.TeacherRole(role), name, email, version)
	if err != nil {
		return s.teacherProblem(opLibrarySetTeacher, err)
	}
	out := librarySubject(sub)
	return LibraryTeacherResult{Subject: &out}
}

// ClearSubjectTeacher removes the teacher of role of a subject while it is still at version.
func (s *LibraryService) ClearSubjectTeacher(ctx context.Context, subjectID string, version int64, role string) LibraryTeacherResult {
	sub, err := s.uc.ClearTeacher(ctx, shared.SubjectID(subjectID), subject.TeacherRole(role), version)
	if err != nil {
		return s.teacherProblem(opLibraryClearTeacher, err)
	}
	out := librarySubject(sub)
	return LibraryTeacherResult{Subject: &out}
}

// teacherProblem maps a use case error to a code of the page. The cause is never logged: a storage error may carry
// row data, and the teacher is personal data.
func (s *LibraryService) teacherProblem(op string, err error) LibraryTeacherResult {
	switch {
	case errors.Is(err, library.ErrInvalidTeacherName):
		return LibraryTeacherResult{Problem: teacherInvalid, Field: fieldTeacherName}
	case errors.Is(err, library.ErrInvalidTeacherEmail):
		return LibraryTeacherResult{Problem: teacherInvalid, Field: fieldTeacherEmail}
	case errors.Is(err, library.ErrInvalidTeacherRole):
		return LibraryTeacherResult{Problem: teacherInvalid, Field: fieldTeacherRole}
	case errors.Is(err, library.ErrInvalidTeacher):
		return LibraryTeacherResult{Problem: teacherInvalid}
	case errors.Is(err, library.ErrSubjectChanged):
		s.log.Info(op, "result", teacherChanged)
		return LibraryTeacherResult{Problem: teacherChanged}
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrInvalidRequest):
		s.log.Info(op, "result", teacherNotFound)
		return LibraryTeacherResult{Problem: teacherNotFound}
	case errors.Is(err, library.ErrUnavailable):
		s.log.Error(op, "result", teacherFailed, "cause", "local storage")
		return LibraryTeacherResult{Problem: teacherFailed}
	}
	s.log.Error(op, "result", teacherFailed, "cause", "unexpected")
	return LibraryTeacherResult{Problem: teacherFailed}
}

// libraryTeacher is nil for no teacher.
func libraryTeacher(t subject.Teacher) *LibraryTeacher {
	if t.IsZero() {
		return nil
	}
	return &LibraryTeacher{Name: t.Name(), Email: t.Email()}
}
