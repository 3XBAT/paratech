package app

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// mentionObjects is the agentturn.Mentioned port (ADR-0028): a mentioned subject or material is in the library (a
// material not removed), a lesson in the schedule (not deleted), an assignment not deleted. A malformed or missing
// ID, or another kind, does not exist.
type mentionObjects struct {
	subjects    interface{ Subjects(ctx context.Context) ([]*subject.Subject, error) }
	materials   interface{ Material(ctx context.Context, id shared.MaterialID) (*material.Material, error) }
	lessons     interface{ Get(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error) }
	assignments interface {
		Assignment(ctx context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error)
	}
}

// Exists reports whether the object of ref exists.
func (m mentionObjects) Exists(ctx context.Context, ref values.Ref) (bool, error) {
	switch ref.Kind() {
	case values.KindSubject:
		id := shared.SubjectID(ref.ID())
		list, err := m.subjects.Subjects(ctx)
		if err != nil {
			return false, err
		}
		for _, s := range list {
			if s.ID() == id {
				return true, nil
			}
		}
		return false, nil
	case values.KindMaterial:
		mat, err := m.materials.Material(ctx, shared.MaterialID(ref.ID()))
		if errors.Is(err, library.ErrInvalidRequest) {
			return false, nil
		}
		if ok, err := found(err); !ok {
			return false, err
		}
		return mat.Active(), nil
	case values.KindLesson:
		id := shared.LessonID(ref.ID())
		if id.Validate() != nil {
			return false, nil
		}
		l, err := m.lessons.Get(ctx, id)
		if ok, err := found(err); !ok {
			return false, err
		}
		return !l.Deleted(), nil
	case values.KindAssignment:
		id := shared.AssignmentID(ref.ID())
		if id.Validate() != nil {
			return false, nil
		}
		_, err := m.assignments.Assignment(ctx, id) // a deleted one is not found too
		return found(err)
	}
	return false, nil
}

// found maps a read error: nil is found, shared.ErrNotFound is not found without an error, anything else fails.
func found(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	}
	return false, err
}
