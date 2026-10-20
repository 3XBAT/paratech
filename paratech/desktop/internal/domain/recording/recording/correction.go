package recording

import (
	"fmt"
	"time"

	"paratech/desktop/internal/domain/recording/values"
)

// LessonCorrection returns the subject and kind of the lesson the user set (TASK-091); the zero value when none.
func (r *Recording) LessonCorrection() values.LessonCorrection { return r.correction }

// CorrectLesson stores the subject and kind of the lesson the user set in the knowledge base (TASK-091); the zero
// correction clears it. The lesson snapshot is not touched. A recording in progress is refused with
// ErrInvalidTransition (its session saves it all the time), one marked for deletion with ErrDeleting. It reports
// whether anything changed.
func (r *Recording) CorrectLesson(now time.Time, c values.LessonCorrection) (bool, error) {
	switch {
	case r.status.InProgress():
		return false, fmt.Errorf("%w: lesson correction of a %s recording", ErrInvalidTransition, r.status)
	case r.Deleting():
		return false, fmt.Errorf("%w: %s", ErrDeleting, r.id)
	case now.IsZero():
		return false, fmt.Errorf("%w: no correction time", ErrInvalidRecording)
	case c == r.correction:
		return false, nil
	}
	r.correction, r.updatedAt = c, now
	return true, nil
}

// InKnowledgeBase reports whether the recording is a lesson of the knowledge base (TASK-091): it is COMPLETED with a
// track that is not silent and it is not marked for deletion. A failed, silent or unfinished recording holds no text
// to read; a deleted one leaves the base with its rows (ADR-0018).
func (r *Recording) InKnowledgeBase() bool {
	return r.status == StatusCompleted && !r.audio.IsZero() && !r.silent && !r.Deleting()
}
