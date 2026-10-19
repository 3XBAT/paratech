package nextlesson

import (
	"strings"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// Window bounds how far after a lesson its next lesson is looked for; beyond it the next class is unknown
// (a holiday break, the end of the term).
const Window = 60 * 24 * time.Hour

// Current is the lesson whose next lesson is wanted, as the caller knows it (the recording's snapshot or the schedule).
type Current struct {
	ID      shared.LessonID // empty: not in the schedule
	Subject string
	Title   string // gives the subject when Subject is empty (lesson.ClassifyTitle)
	Start   time.Time
}

// SubjectKey is the form two subjects are compared in: the subject, else the one derived from the title, in lower
// case with "ё" as "е" and single spaces. Empty means the subject is unknown and no lesson is "the same subject".
func SubjectKey(subject, title string) string {
	s := lesson.CleanText(subject, lesson.MaxSubjectRunes)
	if s == "" {
		s, _ = lesson.ClassifyTitle(title)
	}
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.Join(strings.Fields(s), " ")
}

// Known reports whether c has a subject and a start to look for a next lesson of.
func (c Current) Known() bool { return !c.Start.IsZero() && SubjectKey(c.Subject, c.Title) != "" }

// Next returns the earliest live lesson of lessons with the subject of c that starts after c.Start and before
// c.Start+Window, other than c itself; nil when there is none or the subject of c is unknown. Equal starts pick the
// smaller ID, so the answer does not depend on the order of lessons.
func Next(lessons []*lesson.Lesson, c Current) *lesson.Lesson {
	if !c.Known() {
		return nil
	}
	key := SubjectKey(c.Subject, c.Title)
	limit := c.Start.Add(Window)
	var best *lesson.Lesson
	for _, l := range lessons {
		if l == nil || l.Deleted() || (c.ID != "" && l.ID() == c.ID) {
			continue
		}
		content := l.Content()
		if !content.Start.After(c.Start) || !content.Start.Before(limit) {
			continue
		}
		if SubjectKey(content.Subject, content.Title) != key {
			continue
		}
		if best == nil || earlier(l, best) {
			best = l
		}
	}
	return best
}

func earlier(a, b *lesson.Lesson) bool {
	as, bs := a.Content().Start, b.Content().Start
	if !as.Equal(bs) {
		return as.Before(bs)
	}
	return a.ID() < b.ID()
}
