package homeworkflow

import (
	"context"
	"fmt"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

const (
	opListAssignments = "homeworkflow: list assignments"
	opCorrectLesson   = "homeworkflow: correct the lesson of an assignment"
)

// Assignment returns an assignment for the UI and the agent (PRD section 10). A missing one wraps shared.ErrNotFound;
// so does a deleted one, which every reader skips (ADR-0029: the agent gets unknown_id), and it wraps
// assignment.ErrDeleted as well for the callers that tell the two apart.
func (s *Service) Assignment(ctx context.Context, id shared.AssignmentID) (AssignmentView, error) {
	a, err := s.assignments.Get(ctx, id)
	if err != nil {
		return AssignmentView{}, err
	}
	if a.Deleted() {
		return AssignmentView{}, fmt.Errorf("assignment %s: %w: %w", id, shared.ErrNotFound, assignment.ErrDeleted)
	}
	return s.view(ctx, s.lessons, a)
}

// LessonLabel returns what an assignment shows of its lesson (the subject and the teacher), for the title page of a
// turnkey work (TASK-131; the turnkey LessonLabels port, wired by the app). A missing one wraps shared.ErrNotFound.
func (s *Service) LessonLabel(ctx context.Context, id shared.AssignmentID) (values.LessonLabel, error) {
	a, err := s.assignments.Get(ctx, id)
	if err != nil {
		return values.LessonLabel{}, err
	}
	return s.lessons.label(ctx, a)
}

// ListAssignments returns the newest assignments for the homework screen, at most MaxListedAssignments (PRD section
// 10 step 1); deleted ones are not listed (ADR-0029). It is also what «Сегодня», the subject page and the agent's
// list_assignments read. None is an empty list.
func (s *Service) ListAssignments(ctx context.Context) ([]AssignmentView, error) {
	as, err := s.list.ListRecent(ctx, MaxListedAssignments)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opListAssignments, err)
	}
	lessons := s.lessons.cached()
	out := make([]AssignmentView, 0, len(as))
	for _, a := range as {
		if a.Deleted() { // the store skips them already; a second guard for the readers that rely on it
			continue
		}
		v, err := s.view(ctx, lessons, a)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opListAssignments, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// view is the assignment with what it shows of its lesson.
func (s *Service) view(ctx context.Context, lessons lessonFinder, a *assignment.Assignment) (AssignmentView, error) {
	l, err := lessons.label(ctx, a)
	if err != nil {
		return AssignmentView{}, err
	}
	return assignmentViewOf(a, l), nil
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
	return s.changeAssignment(ctx, e.ID, func(a *assignment.Assignment) (bool, error) { return true, a.Edit(d, s.clock.Now()) },
		fmt.Sprintf("edit assignment %s", e.ID))
}

// CorrectLesson stores the subject and teacher the user entered for an assignment (TASK-092): they win over the
// schedule and the transcript hint; an empty field removes the correction of that field. The texts are cleaned to one
// line of at most values.MaxNameRunes characters. A missing assignment wraps shared.ErrNotFound.
func (s *Service) CorrectLesson(ctx context.Context, c LessonCorrection) (AssignmentView, error) {
	names := values.NewLessonNames(c.Subject, c.Teacher)
	return s.changeAssignment(ctx, c.ID, func(a *assignment.Assignment) (bool, error) {
		return a.Correct(names, s.clock.Now()), nil
	}, fmt.Sprintf("%s %s", opCorrectLesson, c.ID))
}

// changeAssignment loads an assignment, applies one aggregate method and saves it when it changed, in one
// transaction, and returns the view of the result. A deleted assignment is not changed (assignment.ErrDeleted).
func (s *Service) changeAssignment(ctx context.Context, id shared.AssignmentID, apply func(*assignment.Assignment) (bool, error),
	op string) (AssignmentView, error) {
	var a *assignment.Assignment
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if a, err = s.assignments.Get(ctx, id); err != nil {
			return err
		}
		if err := a.NotDeleted(); err != nil {
			return err
		}
		changed, err := apply(a)
		if err != nil || !changed {
			return err
		}
		return s.assignments.Save(ctx, a)
	})
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", op, err)
	}
	v, err := s.view(ctx, s.lessons, a)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", op, err)
	}
	return v, nil
}
