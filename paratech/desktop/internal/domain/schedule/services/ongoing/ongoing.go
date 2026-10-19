package ongoing

import (
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
)

// Current returns the lesson in progress at now (lesson.InProgress), or nil. Of overlapping lessons it picks the one
// that started last, as a newer lesson's popup replaces an older one's; equal starts pick the smaller ID.
func Current(lessons []*lesson.Lesson, now time.Time) *lesson.Lesson {
	var best *lesson.Lesson
	for _, l := range lessons {
		if l == nil || !l.InProgress(now) {
			continue
		}
		if best == nil || later(l, best) {
			best = l
		}
	}
	return best
}

// later reports whether a is preferred to b: a later start, then the smaller ID.
func later(a, b *lesson.Lesson) bool {
	as, bs := a.Content().Start, b.Content().Start
	if !as.Equal(bs) {
		return as.After(bs)
	}
	return a.ID() < b.ID()
}
