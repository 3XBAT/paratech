package lesson

import "paratech/desktop/internal/domain/shared"

// Lesson is one schedule entry (PRD section 11). Skeleton: see the package comment.
type Lesson struct {
	id shared.LessonID
}

// ID returns the lesson identifier.
func (l *Lesson) ID() shared.LessonID { return l.id }
