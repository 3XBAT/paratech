package homeworkflow

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

const (
	opCreateAssignment = "homeworkflow: create assignment"
	opRemoveCreated    = "homeworkflow: remove created assignment"
)

// CreateAssignment stores an assignment or a reminder the student adds by hand or through the agent (ADR-0029). The
// deadline is read in the zone of the use case's clock. A subject is resolved to its current library name and stored
// as the student's correction; without one, a lesson shows the lesson's subject and no lesson shows none. Repeating
// the same ID with the same content returns the stored assignment and writes nothing; other content under that ID,
// or an extracted assignment with it, is shared.ErrDuplicate.
// Errors: invalid texts or deadline wrap assignment.ErrInvalidDetails (assignment.ErrInvalidTitle, ErrInvalidNote,
// ErrInvalidDue, ErrInvalidTime); an unknown subject is ErrSubjectNotFound; an unknown lesson wraps shared.ErrNotFound.
func (s *Service) CreateAssignment(ctx context.Context, in NewAssignment) (AssignmentView, error) {
	// The input is checked before the stored row is looked up, because the repeat check compares the built candidate.
	// So a repeat long after the first call can fail the due window, a removed lesson or a removed subject instead of
	// returning the stored assignment; repeats are meant for retries within seconds.
	now := s.clock.Now()
	due, err := assignment.StudentDue(in.Date, in.Time, now.Location())
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}
	subject, err := s.subjectName(ctx, in.Subject)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}
	if err := s.lessonExists(ctx, in.Lesson); err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}

	id := in.ID
	if id == "" {
		id = s.ids.NewAssignmentID()
	}
	a, err := assignment.NewByStudent(assignment.StudentSpec{
		ID: id, Lesson: in.Lesson, Title: in.Title, Note: in.Note, Due: due, SubjectName: subject,
	}, now)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		stored, err := s.assignments.Get(ctx, id)
		switch {
		case err == nil && stored.SameByStudent(a):
			a = stored // a repeat: the first call wrote it
			return nil
		case err == nil:
			return fmt.Errorf("assignment %s: %w", id, shared.ErrDuplicate)
		case !errors.Is(err, shared.ErrNotFound):
			return err
		}
		return s.assignments.Save(ctx, a)
	})
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}
	v, err := s.view(ctx, s.lessons, a)
	if err != nil {
		return AssignmentView{}, fmt.Errorf("%s: %w", opCreateAssignment, err)
	}
	return v, nil
}

// RemoveCreatedAssignment removes an assignment the student added while it is still at expectedVersion and nothing
// uses it: the Undo of its creation (ADR-0029). An extracted assignment is ErrNotByStudent, a missing one wraps
// shared.ErrNotFound, another version shared.ErrVersionConflict, and one a draft, a delivery, a turnkey work or a live
// proposal references assignment.ErrInUse; in each case nothing is removed.
func (s *Service) RemoveCreatedAssignment(ctx context.Context, id shared.AssignmentID, expectedVersion int64) error {
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		a, err := s.assignments.Get(ctx, id)
		switch {
		case err != nil:
			return err
		case !a.ByStudent():
			return ErrNotByStudent
		case a.Version() != expectedVersion:
			return shared.ErrVersionConflict
		}
		return s.assignments.DeleteUnused(ctx, a)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opRemoveCreated, id, err)
	}
	return nil
}

// subjectName returns the library name of subject id, "" for none.
func (s *Service) subjectName(ctx context.Context, id shared.SubjectID) (string, error) {
	switch {
	case id == "":
		return "", nil
	case s.subjects == nil:
		return "", fmt.Errorf("%w: subject names are not wired", ErrInvalidConfig)
	case id.Validate() != nil:
		return "", ErrSubjectNotFound
	}
	name, err := s.subjects.SubjectName(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "", ErrSubjectNotFound // not wrapped: shared.ErrNotFound means an unknown lesson here
	case err != nil:
		return "", err
	}
	return name, nil
}

// lessonExists checks that lesson id is a live lesson of the schedule; none is fine.
func (s *Service) lessonExists(ctx context.Context, id shared.LessonID) error {
	if id == "" {
		return nil
	}
	if id.Validate() != nil {
		return fmt.Errorf("lesson: %w", shared.ErrNotFound)
	}
	_, err := s.lessons.lessons.Get(ctx, id)
	return err
}
