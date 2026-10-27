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
// proposals stay attached; a proposal of it goes STALE at «Да». One transaction, one aggregate, plus the queue row that
// removes its Google deadline event (ADR-0018 pattern); a sync is requested after the commit. Errors: another version
// is shared.ErrVersionConflict, a missing one wraps shared.ErrNotFound, one already deleted assignment.ErrDeleted, and
// one that something in progress uses (an unresolved delivery, a work in triage or writing, an unfinished draft or
// writing job) assignment.ErrInUse; in each case nothing changes. The view returned is the deleted assignment, for
// «Вернуть».
func (s *Service) DeleteAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (AssignmentView, error) {
	change := func(ctx context.Context, a *assignment.Assignment) error {
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
		now := s.clock.Now()
		if err := a.Delete(now); err != nil {
			return err
		}
		if s.removals == nil {
			return nil
		}
		return s.removals.QueueDeadlineRemoval(ctx, a.ID(), now)
	}
	return s.softDelete(ctx, id, expectedVersion, opDeleteAssignment, change, s.requestSync)
}

// RestoreAssignment is «Вернуть»: the deleted assignment at expectedVersion shows again as it was. A removal of its
// deadline event still waiting is forgotten, so the event stays; after the sync it returns without one. Errors: another
// version is shared.ErrVersionConflict, a missing one wraps shared.ErrNotFound, one that is not deleted
// assignment.ErrNotDeleted; in each case nothing changes.
func (s *Service) RestoreAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (AssignmentView, error) {
	change := func(ctx context.Context, a *assignment.Assignment) error {
		if err := a.Restore(s.clock.Now()); err != nil {
			return err
		}
		if s.removals == nil {
			return nil
		}
		return s.removals.PurgeDeadlineRemoval(ctx, a.ID())
	}
	return s.softDelete(ctx, id, expectedVersion, opRestoreAssignment, change, nil)
}

// requestSync asks for the calendar sync that removes the queued deadline event. A lost request is repaired by the
// next sync: the row stays queued until then.
func (s *Service) requestSync(ctx context.Context) {
	if s.removals != nil && s.sync != nil {
		s.sync.RequestSync(ctx)
	}
}

// softDelete loads the assignment, checks the version, applies change and saves it, in one transaction, calls
// committed (if any) after the commit and returns the view of the result.
func (s *Service) softDelete(ctx context.Context, id shared.AssignmentID, expectedVersion int64, op string,
	change func(context.Context, *assignment.Assignment) error, committed func(context.Context)) (AssignmentView, error) {
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
	if committed != nil {
		committed(ctx)
	}
	v, err := s.view(ctx, s.lessons, a)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s %s: %w", op, id, err)
	}
	return v, nil
}
