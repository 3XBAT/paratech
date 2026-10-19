package homeworkflow

import (
	"context"
	"errors"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// assignmentLesson is what the schedule says about the lesson of an assignment (TASK-092).
type assignmentLesson struct {
	title string
	sched values.ScheduledLesson
}

// lessonFinder reads the lesson of an assignment: the snapshot its recording kept at the start (TASK-088), reached via
// the transcript; only without a snapshot the live lesson of the schedule. A missing transcript, recording or lesson
// leaves the fields empty; other read errors are returned.
type lessonFinder struct {
	recordings TranscriptRecordings
	lessons    LessonReader
	clock      Clock

	cache map[lessonKey]assignmentLesson // optional, for a list of assignments
}

type lessonKey struct {
	transcript shared.TranscriptID
	lesson     shared.LessonID
}

// cached returns a finder that reads the lesson of each transcript and lesson once.
func (f lessonFinder) cached() lessonFinder {
	f.cache = map[lessonKey]assignmentLesson{}
	return f
}

func (f lessonFinder) find(ctx context.Context, a *assignment.Assignment) (assignmentLesson, error) {
	key := lessonKey{transcript: a.TranscriptID(), lesson: a.LessonID()}
	if l, ok := f.cache[key]; ok {
		return l, nil
	}
	l, err := f.read(ctx, key)
	if err != nil {
		return assignmentLesson{}, err
	}
	if f.cache != nil {
		f.cache[key] = l
	}
	return l, nil
}

// read gathers the snapshot and the live lesson; values.ResolveScheduledLesson decides what is shown.
func (f lessonFinder) read(ctx context.Context, key lessonKey) (assignmentLesson, error) {
	var out assignmentLesson
	var snapshot, live *values.ScheduledLesson
	var recorded time.Time

	if key.transcript != "" {
		rec, err := f.recordings.RecordingOf(ctx, key.transcript)
		switch {
		case err == nil:
			recorded = rec.StartedAt()
			if snap := rec.LessonSnapshot(); !snap.IsZero() {
				out.title = snap.Title()
				snapshot = &values.ScheduledLesson{
					Subject: snap.Subject(), Kind: snap.Kind(), Teacher: snap.Teacher(), Start: snap.ScheduledStart(),
				}
			}
		case !errors.Is(err, shared.ErrNotFound):
			return assignmentLesson{}, err
		}
	}

	if snapshot == nil && key.lesson != "" {
		l, err := f.lessons.Get(ctx, key.lesson)
		switch {
		case err == nil:
			c := l.Content()
			out.title = c.Title
			live = &values.ScheduledLesson{Subject: c.Subject, Kind: c.Kind.String(), Teacher: c.Teacher, Start: c.Start}
		case !errors.Is(err, shared.ErrNotFound): // a lesson that left the schedule names nothing
			return assignmentLesson{}, err
		}
	}
	out.sched = values.ResolveScheduledLesson(snapshot, live, recorded, f.clock.Now().Location())
	return out, nil
}

// label resolves what the assignment shows of its lesson (values.NewLessonLabel).
func (f lessonFinder) label(ctx context.Context, a *assignment.Assignment) (values.LessonLabel, error) {
	l, err := f.find(ctx, a)
	if err != nil {
		return values.LessonLabel{}, err
	}
	return values.NewLessonLabel(l.sched, a.Hints(), a.Correction()), nil
}
