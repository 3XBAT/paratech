package notes

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// ForLesson returns the active notes of a lecture, as made: of a recording, of a schedule lesson, or of a day of a
// subject. The day holds the notes anchored to it and those whose recording or lesson is gone and that read as it.
func (s *Service) ForLesson(ctx context.Context, l Lecture) ([]View, error) {
	list, keep, err := s.lectureList(ctx, l)
	if err != nil {
		return nil, err
	}
	return s.activeViews(ctx, list, keep)
}

// lectureList reads the stored notes of the lecture l, in any status, and the test of the current anchor a note of
// the list must pass (nil: none).
func (s *Service) lectureList(ctx context.Context, l Lecture) (list []*note.Note, keep func(note.Anchor) bool, err error) {
	switch {
	case l.Recording != "" && l.Lesson == "" && l.Subject == "" && l.Date.IsZero():
		if l.Recording.Validate() != nil {
			return nil, nil, ErrInvalidRequest
		}
		list, err = s.notes.ListByRecording(ctx, l.Recording)
	case l.Lesson != "" && l.Recording == "" && l.Subject == "" && l.Date.IsZero():
		if l.Lesson.Validate() != nil {
			return nil, nil, ErrInvalidRequest
		}
		list, err = s.notes.ListByLesson(ctx, l.Lesson)
	case l.Subject != "" && !l.Date.IsZero() && l.Recording == "" && l.Lesson == "":
		if l.Subject.Validate() != nil {
			return nil, nil, ErrInvalidRequest
		}
		list, err = s.notes.ListBySubject(ctx, l.Subject)
		keep = func(a note.Anchor) bool { return a.Kind() == note.AnchorDay && a.Date() == l.Date }
	default:
		return nil, nil, ErrInvalidRequest
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return list, keep, nil
}

// ForSubject returns the active notes of a subject by the date of the anchor (undated last), then as made.
func (s *Service) ForSubject(ctx context.Context, sub shared.SubjectID) ([]View, error) {
	if sub.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	list, err := s.notes.ListBySubject(ctx, sub)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return s.activeViews(ctx, list, nil)
}

// activeViews reads the active notes of list whose current anchor keep accepts (nil: all), in order.
func (s *Service) activeViews(ctx context.Context, list []*note.Note, keep func(note.Anchor) bool) ([]View, error) {
	r := s.newResolver()
	out := []View{}
	for _, n := range list {
		if !n.Active() {
			continue
		}
		a, err := r.current(ctx, n.Anchor())
		if err != nil {
			return nil, err
		}
		if keep != nil && !keep(a) {
			continue
		}
		v, err := s.viewWith(ctx, n, a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// view reads a note as it is now.
func (s *Service) view(ctx context.Context, n *note.Note) (View, error) {
	a, err := s.newResolver().current(ctx, n.Anchor())
	if err != nil {
		return View{}, err
	}
	return s.viewWith(ctx, n, a)
}

func (s *Service) viewWith(ctx context.Context, n *note.Note, a note.Anchor) (View, error) {
	ids := n.Parts()
	v := View{Note: n, Anchor: a, Parts: make([]*material.Material, 0, len(ids))}
	for _, id := range ids {
		m, err := s.materials.Material(ctx, id)
		switch {
		case errors.Is(err, shared.ErrNotFound):
			m = nil
		case err != nil:
			return View{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		v.Parts = append(v.Parts, m)
	}
	return v, nil
}
