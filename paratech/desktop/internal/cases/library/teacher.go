package library

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// SetTeacher sets the teacher of role (lecture, practice or lab, ADR-0030) of a subject while the subject is still at
// expectedVersion, the version the student saw. It is the student's action only: no agent tool reaches it, and the
// teachers never go into the agent's base or to the Hub. An unknown role is ErrInvalidTeacherRole, a bad name or
// address ErrInvalidTeacherName or ErrInvalidTeacherEmail (checked before the subject is read), another version
// ErrSubjectChanged; the same teacher again changes nothing and keeps the version. Nothing is logged: the teachers
// are personal data.
func (s *Service) SetTeacher(ctx context.Context, id shared.SubjectID, role subject.TeacherRole, name, email string,
	expectedVersion int64) (*subject.Subject, error) {
	if !role.Valid() {
		return nil, ErrInvalidTeacherRole
	}
	if _, err := subject.NewTeacher(name, email); err != nil {
		return nil, domainError(err)
	}
	return s.changeSubjectAt(ctx, id, expectedVersion, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.SetTeacher(role, name, email, s.clock.Now())
		return changed, domainError(err)
	})
}

// ClearTeacher removes the teacher of role of a subject while it is still at expectedVersion; an unknown role is
// ErrInvalidTeacherRole, another version ErrSubjectChanged, a role without a teacher returns the subject unchanged.
func (s *Service) ClearTeacher(ctx context.Context, id shared.SubjectID, role subject.TeacherRole,
	expectedVersion int64) (*subject.Subject, error) {
	if !role.Valid() {
		return nil, ErrInvalidTeacherRole
	}
	return s.changeSubjectAt(ctx, id, expectedVersion, func(sub *subject.Subject) (bool, error) {
		changed, err := sub.ClearTeacher(role, s.clock.Now())
		return changed, domainError(err)
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
