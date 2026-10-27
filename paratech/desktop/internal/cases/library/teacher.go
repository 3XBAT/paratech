package library

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// SetTeacher sets the teacher of a subject (ADR-0030) while the subject is still at expectedVersion, the version the
// student saw. It is the student's action only: no agent tool reaches it, and the teacher never goes into the agent's
// base or to the Hub. A bad name or address is ErrInvalidTeacherName or ErrInvalidTeacherEmail (checked before the
// subject is read), another version ErrSubjectChanged; the same teacher again changes nothing and keeps the version.
// Nothing is logged: the teacher is personal data.
func (s *Service) SetTeacher(ctx context.Context, id shared.SubjectID, name, email string, expectedVersion int64) (*subject.Subject, error) {
	if _, err := subject.NewTeacher(name, email); err != nil {
		return nil, domainError(err)
	}
	return s.changeSubjectAt(ctx, id, expectedVersion, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.SetTeacher(name, email, s.clock.Now())
		return changed, domainError(err)
	})
}

// ClearTeacher removes the teacher of a subject while it is still at expectedVersion; another version is
// ErrSubjectChanged, a subject without a teacher is returned unchanged.
func (s *Service) ClearTeacher(ctx context.Context, id shared.SubjectID, expectedVersion int64) (*subject.Subject, error) {
	return s.changeSubjectAt(ctx, id, expectedVersion, func(sub *subject.Subject) (bool, error) {
		return sub.ClearTeacher(s.clock.Now()), nil
	})
}

// changeSubjectAt loads the subject, applies fn when it is at expectedVersion and saves it when fn changed it. Unlike
// changeSubject it never tries again: a save that lost is a change the student has not seen, so ErrSubjectChanged.
func (s *Service) changeSubjectAt(ctx context.Context, id shared.SubjectID, expectedVersion int64,
	fn func(*subject.Subject) (bool, error)) (*subject.Subject, error) {
	sub, err := s.subject(ctx, id)
	if err != nil {
		return nil, err
	}
	if sub.Version() != expectedVersion {
		return nil, ErrSubjectChanged
	}
	changed, err := fn(sub)
	switch {
	case err != nil:
		return nil, err
	case !changed:
		return sub, nil
	}
	switch err := s.subjects.Save(ctx, sub); {
	case errors.Is(err, shared.ErrVersionConflict):
		return nil, ErrSubjectChanged
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return sub, nil
}
