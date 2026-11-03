package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/shared"
)

// Problems of DiscardSubject and RestoreSubject as the page reads them; "" is success. The page owns the Russian texts.
const (
	subjectChanged  = "changed"   // the subject is not at the version the page showed: reload
	subjectNotFound = "not_found" // no such subject (also an invalid id, a removed one for DiscardSubject, a purged one)
	subjectExpired  = "expired"   // «Вернуть» after subject.RestoreWindow: the subject is purged (or soon)
	subjectFailed   = "failed"
)

// RemovedNameTaken is LibraryRemovedNameError.Problem.
const RemovedNameTaken = "removed_name_taken"

// LibraryDiscard answers DiscardSubject: Problem "" with what went with the subject (Notes active notes, Files files
// that are no part of them) and Version, the removed subject's version for RestoreSubject; or a problem ("changed",
// "not_found", "failed") with zeros.
type LibraryDiscard struct {
	Problem string `json:"problem"`
	Notes   int    `json:"notes"`
	Files   int    `json:"files"`
	Version int64  `json:"version"`
}

// LibraryRestore answers RestoreSubject: Problem "" with the subject back, or a problem ("expired", "changed",
// "not_found", "failed") with Subject null.
type LibraryRestore struct {
	Problem string          `json:"problem"`
	Subject *LibrarySubject `json:"subject"`
}

// LibraryRemovedNameError is the refusal of CreateSubject when a removed subject holds the name or an alias: the page
// offers «Вернуть» for SubjectID at Version (RestoreSubject). Its text is the one of a plain taken name, so a page that
// does not know it still shows that; Wails passes the fields to the page as the rejection's cause.
type LibraryRemovedNameError struct {
	Problem   string `json:"problem"`
	SubjectID string `json:"subjectId"`
	Version   int64  `json:"version"`
}

// Error implements error.
func (e *LibraryRemovedNameError) Error() string { return ErrLibraryNameTaken.Error() }

// Unwrap makes errors.Is find ErrLibraryNameTaken.
func (e *LibraryRemovedNameError) Unwrap() error { return ErrLibraryNameTaken }

// subjectProblem maps an error of DiscardSubject or RestoreSubject to a code of the page; an unexpected one is logged
// with its class only, as by fail.
func (s *LibraryService) subjectProblem(op string, err error) string {
	code := subjectFailed
	switch {
	case errors.Is(err, library.ErrRestoreExpired):
		code = subjectExpired
	case errors.Is(err, library.ErrSubjectChanged):
		code = subjectChanged
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrInvalidRequest):
		code = subjectNotFound
	}
	if code == subjectFailed {
		s.log.Error(op, "problem", code, "cause", libraryCause(err))
	} else {
		s.log.Info(op, "problem", code)
	}
	return code
}

// removedName is the refusal of a name the removed subject id holds, with its version for «Вернуть». One no longer
// removed (restored or purged meanwhile) is ErrLibraryBusy: a repeat answers as the library is now.
func (s *LibraryService) removedName(ctx context.Context, id shared.SubjectID) error {
	sub, err := s.uc.RemovedSubject(ctx, id)
	switch {
	case errors.Is(err, library.ErrSubjectNotFound):
		s.log.Info(opLibraryCreate, "result", "removed subject gone")
		return ErrLibraryBusy
	case err != nil:
		return s.fail(opLibraryRemovedSub, err)
	}
	s.log.Info(opLibraryCreate, "result", RemovedNameTaken)
	return &LibraryRemovedNameError{Problem: RemovedNameTaken, SubjectID: string(sub.ID()), Version: sub.Version()}
}

// removedVersions sets the version of the removed subject of each skipped row for «Вернуть». A row whose subject is no
// longer removed, or cannot be read, loses it and stays a plain name_taken row: no button that cannot work.
func (s *LibraryService) removedVersions(ctx context.Context, rows []ScheduleSkippedRow) {
	for i := range rows {
		r := &rows[i]
		if r.RemovedSubjectID == "" {
			continue
		}
		sub, err := s.uc.RemovedSubject(ctx, shared.SubjectID(r.RemovedSubjectID))
		if err != nil {
			if !errors.Is(err, library.ErrSubjectNotFound) {
				s.log.Warn(opLibraryRemovedSub, "cause", libraryCause(err))
			}
			r.RemovedSubjectID = ""
			continue
		}
		r.RemovedSubjectVersion = sub.Version()
	}
}
