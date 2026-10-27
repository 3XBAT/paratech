package notes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// anchorFor checks the lecture the student chose through the ports and builds the anchor with its snapshot.
func (s *Service) anchorFor(ctx context.Context, ref AnchorRef) (note.Anchor, error) {
	spec := note.AnchorSpec{Kind: ref.Kind}
	switch ref.Kind {
	case note.AnchorRecording:
		rec, err := s.recording(ctx, ref.Recording)
		if err != nil {
			return note.Anchor{}, err
		}
		if rec == nil || rec.Status() != recording.StatusCompleted || rec.Deleting() {
			return note.Anchor{}, ErrRecordingUnavailable
		}
		snap := rec.LessonSnapshot()
		kind := rec.LessonCorrection().Kind()
		if kind == "" {
			kind = snap.Kind()
		}
		date, err := s.localDate(rec.StartedAt())
		if err != nil {
			return note.Anchor{}, err
		}
		spec.Recording, spec.Date, spec.LessonKind, spec.Title = rec.ID(), date, kind, snap.Title()
	case note.AnchorLesson:
		l, err := s.lesson(ctx, ref.Lesson)
		if err != nil {
			return note.Anchor{}, err
		}
		if l == nil {
			return note.Anchor{}, ErrLessonNotFound
		}
		c := l.Content()
		date, err := s.localDate(c.Start)
		if err != nil {
			return note.Anchor{}, err
		}
		spec.Lesson, spec.Date, spec.Title = l.ID(), date, c.Title
		if c.Kind != lesson.KindUnknown {
			spec.LessonKind = c.Kind.String()
		}
	case note.AnchorDay, note.AnchorUndated:
		spec.Date = ref.Date // NewAnchor refuses an undated anchor with a date
	}
	a, err := note.NewAnchor(spec)
	if err != nil {
		return note.Anchor{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return a, nil
}

// localDate is the date of t in the location of the clock.
func (s *Service) localDate(t time.Time) (note.Date, error) {
	t = t.In(s.clock.Now().Location())
	d, err := note.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		return note.Date{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	return d, nil
}

// recording loads a recording; nil when it is gone.
func (s *Service) recording(ctx context.Context, id shared.RecordingID) (*recording.Recording, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	rec, err := s.recordings.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return rec, nil
}

// lesson loads a live lesson; nil when it is gone.
func (s *Service) lesson(ctx context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if id.Validate() != nil {
		return nil, ErrInvalidRequest
	}
	l, err := s.lessons.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if l.Deleted() {
		return nil, nil
	}
	return l, nil
}

// resolver reads anchors as they are now, asking for each recording and lesson once per call.
type resolver struct {
	s          *Service
	recordings map[shared.RecordingID]bool // true: still there
	lessons    map[shared.LessonID]bool
}

func (s *Service) newResolver() *resolver {
	return &resolver{s: s, recordings: map[shared.RecordingID]bool{}, lessons: map[shared.LessonID]bool{}}
}

// current is the anchor as it reads now: a recording that is gone or marked for deletion (ADR-0018), or a lesson that
// left the schedule, reads as the day of its snapshot.
func (r *resolver) current(ctx context.Context, a note.Anchor) (note.Anchor, error) {
	switch a.Kind() {
	case note.AnchorRecording:
		live, ok := r.recordings[a.Recording()]
		if !ok {
			rec, err := r.s.recording(ctx, a.Recording())
			if err != nil {
				return note.Anchor{}, err
			}
			live = rec != nil && !rec.Deleting()
			r.recordings[a.Recording()] = live
		}
		if !live {
			return a.FallBackToDay(), nil
		}
	case note.AnchorLesson:
		live, ok := r.lessons[a.Lesson()]
		if !ok {
			l, err := r.s.lesson(ctx, a.Lesson())
			if err != nil {
				return note.Anchor{}, err
			}
			live = l != nil
			r.lessons[a.Lesson()] = live
		}
		if !live {
			return a.FallBackToDay(), nil
		}
	}
	return a, nil
}
