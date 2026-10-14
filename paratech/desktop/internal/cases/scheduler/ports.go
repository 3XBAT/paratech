package scheduler

import (
	"context"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
)

// Lessons reads the local schedule and stores the "lesson is starting" mark (PRD sections 6.1 and 11.1). Implemented
// by the sqlite adapter (LessonRepository).
type Lessons interface {
	// ListStartingBetween returns the live lessons whose start lies in [from, to), ordered by start. Soft-deleted
	// lessons are not returned.
	ListStartingBetween(ctx context.Context, from, to time.Time) ([]*lesson.Lesson, error)

	// Save writes the lesson with the version compare-and-swap of ADR-0004(d): a lesson changed meanwhile yields an
	// error wrapping shared.ErrVersionConflict and nothing is written.
	Save(ctx context.Context, l *lesson.Lesson) error
}

// Clock supplies the current time (ARCH-24). Implemented by the clock adapter and by a controllable fake in tests.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// Sleeper waits on the same time source as Clock (ARCH-24), between two polls of the schedule.
type Sleeper interface {
	// Sleep blocks until d has passed and returns nil, or until ctx ends and returns its error.
	Sleep(ctx context.Context, d time.Duration) error
}

// Events receives the "lesson is starting" events (PRD section 6.1). Implemented by the consumer that shows the
// recording prompt (TASK-022). It must not block for long: Tick waits for it.
type Events interface {
	// LessonStarting delivers one announcement. An error is logged and the announcement is not repeated.
	LessonStarting(ctx context.Context, e lesson.StartAnnounced) error
}
