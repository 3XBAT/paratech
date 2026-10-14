package homeworkflow

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// Assignment returns an assignment for the UI (PRD section 10). A missing one wraps shared.ErrNotFound.
func (s *Service) Assignment(ctx context.Context, id shared.AssignmentID) (AssignmentView, error) {
	a, err := s.assignments.Get(ctx, id)
	if err != nil {
		return AssignmentView{}, err
	}
	return assignmentViewOf(a), nil
}

// EditAssignment stores what the user entered for an extracted assignment and its deadline (PRD section 10, AC-13).
// From then on reprocessing the transcript never overwrites it (assignment.Edit). The user's values win over a
// reprocessing job that saved meanwhile: the load and the save run in one transaction. Invalid details wrap
// assignment.ErrInvalidDetails and change nothing.
func (s *Service) EditAssignment(ctx context.Context, e AssignmentEdit) (AssignmentView, error) {
	d, err := assignment.NewDetails(e.Title, e.Description, e.Due)
	if err != nil {
		return AssignmentView{}, err
	}

	var view AssignmentView
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		a, err := s.assignments.Get(ctx, e.ID)
		if err != nil {
			return err
		}
		if err := a.Edit(d, s.clock.Now()); err != nil {
			return err
		}
		if err := s.assignments.Save(ctx, a); err != nil {
			return err
		}
		view = assignmentViewOf(a)
		return nil
	})
	if err != nil {
		return AssignmentView{}, fmt.Errorf("edit assignment %s: %w", e.ID, err)
	}
	return view, nil
}
