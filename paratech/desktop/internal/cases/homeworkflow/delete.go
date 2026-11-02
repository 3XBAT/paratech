package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const (
	opDeleteAssignment  = "homeworkflow: delete assignment"
	opRestoreAssignment = "homeworkflow: restore assignment"
	opDeletionImpact    = "homeworkflow: what a delete stops"
	opStopNotRead       = "homeworkflow: jobs of a deleted assignment not read"
	opStopFailed        = "homeworkflow: job of a deleted assignment not cancelled"
)

// Deletion answers DeleteAndStop: the deleted assignment, for «Вернуть», and whether the delete stopped what was under
// way for it (a work in triage or writing, a draft or writing job).
type Deletion struct {
	Assignment AssignmentView
	Stopped    bool
}

// DeleteImpact tells what «Удалить» of an assignment would stop, so that the card says it before the click.
type DeleteImpact struct {
	Working bool // a work in triage or writing or an unfinished draft or writing job
}

// DeleteAssignment is DeleteAndStop without the answer whether something was stopped.
func (s *Service) DeleteAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (AssignmentView, error) {
	d, err := s.DeleteAndStop(ctx, id, expectedVersion)
	return d.Assignment, err
}

// DeleteAndStop is «Удалить» of any assignment, whatever its origin (ADR-0029, addendum 2026-10-27): a soft delete
// at expectedVersion, after which every reader skips it until RestoreAssignment. Drafts, works, deliveries and
// proposals stay attached; a proposal of it goes STALE at «Да». One transaction, one aggregate, plus the queue row that
// removes its Google deadline event (ADR-0018 pattern); a sync is requested after the commit.
//
// A work in triage or writing or a draft generation does not keep it (TASK-338): after the commit its unfinished
// homework_draft and work_writing jobs are cancelled. A cancel that fails is logged and the delete stays: the handlers
// refuse a deleted assignment. «Вернуть» brings the assignment back and restarts nothing; the work stays where it
// stopped.
//
// Errors: another version is shared.ErrVersionConflict, a missing one wraps shared.ErrNotFound, one already deleted
// assignment.ErrDeleted, and one with an email that may still go out or may have gone out unseen (a delivery in
// PENDING, SENDING or UNKNOWN, hard rule 7) assignment.ErrInUse; in each case nothing changes and nothing is stopped.
func (s *Service) DeleteAndStop(ctx context.Context, id shared.AssignmentID, expectedVersion int64) (Deletion, error) {
	var working bool
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
		if s.activity != nil {
			if working, err = s.activity.Working(ctx, a.ID()); err != nil {
				return err
			}
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
	committed := func(ctx context.Context) {
		s.requestSync(ctx)
		s.stopJobs(ctx, id)
	}
	v, err := s.softDelete(ctx, id, expectedVersion, opDeleteAssignment, change, committed)
	if err != nil {
		return Deletion{}, err
	}
	return Deletion{Assignment: v, Stopped: working}, nil
}

// DeletionImpact tells whether «Удалить» of assignment id would stop a work or a job. An unknown or deleted one stops
// nothing. Errors: the store could not be read.
func (s *Service) DeletionImpact(ctx context.Context, id shared.AssignmentID) (DeleteImpact, error) {
	if s.activity == nil {
		return DeleteImpact{}, nil
	}
	_, err := liveAssignment(ctx, s.assignments, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return DeleteImpact{}, nil
	case err != nil:
		return DeleteImpact{}, fmt.Errorf("%s %s: %w", opDeletionImpact, id, err)
	}
	working, err := s.activity.Working(ctx, id)
	if err != nil {
		return DeleteImpact{}, fmt.Errorf("%s %s: %w", opDeletionImpact, id, err)
	}
	return DeleteImpact{Working: working}, nil
}

// stopJobs cancels the unfinished jobs naming the deleted assignment id. A job that ended meanwhile has nothing to
// stop; any other failure is only logged.
func (s *Service) stopJobs(ctx context.Context, id shared.AssignmentID) {
	if s.activity == nil || s.jobs == nil {
		return
	}
	ids, err := s.activity.ActiveJobs(ctx, id)
	if err != nil {
		s.log.WarnContext(ctx, opStopNotRead, slog.String("assignment", string(id)), slog.Any("cause", err))
		return
	}
	var failed []shared.JobID
	causes := map[shared.JobID]error{}
	for _, j := range ids {
		if err := s.jobs.Cancel(ctx, j); err != nil {
			failed = append(failed, j)
			causes[j] = err
		}
	}
	if len(failed) == 0 {
		return
	}
	still, err := s.activity.ActiveJobs(ctx, id)
	if err != nil {
		s.log.WarnContext(ctx, opStopNotRead, slog.String("assignment", string(id)), slog.Any("cause", err))
		still = failed
	}
	for _, j := range failed {
		if slices.Contains(still, j) {
			s.log.WarnContext(ctx, opStopFailed, slog.String("assignment", string(id)), slog.String("job", j.String()),
				slog.Any("cause", causes[j]))
		}
	}
}

// RestoreAssignment is «Вернуть»: the deleted assignment at expectedVersion shows again as it was. A removal of its
// deadline event still waiting is forgotten, so the event stays; after the sync it returns without one. A work or a
// draft generation the delete stopped is not started again: the student continues it. Errors: another
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
