package homeworkflow

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const (
	opDeleteAssignment  = "homeworkflow: delete assignment"
	opRestoreAssignment = "homeworkflow: restore assignment"
)

// DeleteAssignment is «Удалить» of any assignment, whatever its origin (ADR-0029, addendum 2026-10-27): a soft delete
// at expectedVersion, after which every reader skips it until RestoreAssignment. Drafts, works, deliveries and
// proposals stay attached. One transaction, one aggregate. Errors: another version is shared.ErrVersionConflict, a
// missing one wraps shared.ErrNotFound, one already deleted assignment.ErrDeleted, and one that something in progress
// uses (an unresolved delivery, a work in triage or writing, an unfinished draft or writing job) assignment.ErrInUse;
// in each case nothing changes. The view returned is the deleted assignment, for «Вернуть».
func (s *Service) DeleteAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (AssignmentView, error) {
	return s.softDelete(ctx, id, expectedVersion, opDeleteAssignment, func(ctx context.Context, a *assignment.Assignment) error {
		if err := a.NotDeleted(); err != nil {
			return err
		}
		busy, err := s.assignments.Busy(ctx, a.ID())
		switch {
		case err != nil:
			return err
		case busy:
			return assignment.ErrInUse
		}
		return a.Delete(s.clock.Now())
	})
}

// RestoreAssignment is «Вернуть»: the deleted assignment at expectedVersion shows again as it was. Errors: another
// version is shared.ErrVersionConflict, a missing one wraps shared.ErrNotFound, one that is not deleted
// assignment.ErrNotDeleted; in each case nothing changes.
func (s *Service) RestoreAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (AssignmentView, error) {
	return s.softDelete(ctx, id, expectedVersion, opRestoreAssignment, func(_ context.Context, a *assignment.Assignment) error {
		return a.Restore(s.clock.Now())
	})
}

// softDelete loads the assignment, checks the version, applies change and saves it, in one transaction, and returns
// the view of the result.
func (s *Service) softDelete(ctx context.Context, id shared.AssignmentID, expectedVersion int64, op string,
	change func(context.Context, *assignment.Assignment) error) (AssignmentView, error) {
	var a *assignment.Assignment
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if a, err = s.assignments.Get(ctx, id); err != nil {
			return err
		}
		if a.Version() != expectedVersion {
			return shared.ErrVersionConflict
		}
		if err := change(ctx, a); err != nil {
			return err
		}
		return s.assignments.Save(ctx, a)
	})
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s %s: %w", op, id, err)
	}
	v, err := s.view(ctx, s.lessons, a)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s %s: %w", op, id, err)
	}
	return v, nil
}
