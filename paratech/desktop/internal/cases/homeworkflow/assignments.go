package homeworkflow

import (
	"context"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

const (
	opListAssignments  = "homeworkflow: list assignments"
	opCorrectLesson    = "homeworkflow: correct the lesson of an assignment"
	opEditAssignment   = "homeworkflow: edit assignment"
	opSetAssignmentDue = "homeworkflow: set the deadline of an assignment"
)

// Assignment returns an assignment for the UI and the agent (PRD section 10). A missing one wraps shared.ErrNotFound;
// so does a deleted one, which every reader skips (ADR-0029: the agent gets unknown_id), and it wraps
// assignment.ErrDeleted as well for the callers that tell the two apart.
func (s *Service) Assignment(ctx context.Context, id shared.AssignmentID) (AssignmentView, error) {
	a, err := liveAssignment(ctx, s.assignments, id)
	if err != nil {
		return AssignmentView{}, err
	}
	return s.view(ctx, s.lessons, a)
}

// liveAssignment reads assignment id for a reader that must not see a deleted one: a deleted one wraps both
// shared.ErrNotFound and assignment.ErrDeleted, as Service.Assignment.
func liveAssignment(ctx context.Context, repo AssignmentRepository, id shared.AssignmentID) (*assignment.Assignment, error) {
	a, err := repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.Deleted() {
		return nil, fmt.Errorf("assignment %s: %w: %w", id, shared.ErrNotFound, assignment.ErrDeleted)
	}
	return a, nil
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

// EditAssignment stores what the student entered for an assignment of any origin: title, note, deadline and subject
// (PRD section 10, AC-13; ADR-0029, addendum 2026-10-27). The deadline is read in the zone of the use case's clock,
// as by CreateAssignment; the subject is resolved to its current library name and stored as the student's subject
// correction, none removes that correction. From then on reprocessing never overwrites the assignment
// (assignment.EditByStudent). The load, the version check and the save run in one transaction.
// Errors: invalid texts or deadline wrap assignment.ErrInvalidDetails (ErrInvalidTitle, ErrInvalidNote, ErrInvalidDue,
// ErrInvalidTime); an unknown subject is ErrSubjectNotFound; a missing assignment wraps shared.ErrNotFound, a deleted
// one assignment.ErrDeleted, another version shared.ErrVersionConflict. Nothing changes then.
func (s *Service) EditAssignment(ctx context.Context, e AssignmentEdit) (AssignmentView, error) {
	op := fmt.Sprintf("%s %s", opEditAssignment, e.ID)
	now := s.clock.Now()
	due, err := assignment.StudentDue(e.Date, e.Time, now.Location())
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", op, err)
	}
	subject, err := s.subjectName(ctx, e.Subject)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", op, err)
	}
	edit := assignment.StudentEdit{Title: e.Title, Note: e.Note, Due: due, SubjectName: subject}
	return s.changeAssignment(ctx, e.ID, func(a *assignment.Assignment) (bool, error) {
		if a.Version() != e.ExpectedVersion {
			return false, shared.ErrVersionConflict
		}
		return true, a.EditByStudent(edit, now)
	}, op)
}

// SetAssignmentDue changes only the deadline of an assignment still at expectedVersion to exactly due (zero: none),
// keeping its title and description: the agent's edit_due and its Undo. The rules are those of assignment.Edit, of
// any origin; the assignment is user-edited from then on. Errors: invalid details wrap assignment.ErrInvalidDetails, a
// missing assignment shared.ErrNotFound, a deleted one assignment.ErrDeleted, another version
// shared.ErrVersionConflict.
func (s *Service) SetAssignmentDue(ctx context.Context, id shared.AssignmentID, expectedVersion int64, due time.Time) (AssignmentView, error) {
	return s.changeAssignment(ctx, id, func(a *assignment.Assignment) (bool, error) {
		if a.Version() != expectedVersion {
			return false, shared.ErrVersionConflict
		}
		d, err := assignment.NewDetails(a.Details().Title(), a.Details().Description(), due)
		if err != nil {
			return false, err
		}
		return true, a.Edit(d, s.clock.Now())
	}, fmt.Sprintf("%s %s", opSetAssignmentDue, id))
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
