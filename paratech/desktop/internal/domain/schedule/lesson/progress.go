package lesson

import "time"

// InProgress reports whether the lesson runs at now: it is live and now lies in [start, end). Times are compared as
// instants, like StartAnnouncementDue.
func (l *Lesson) InProgress(now time.Time) bool {
	if l.Deleted() || now.IsZero() {
		return false
	}
	return !now.Before(l.content.Start) && now.Before(l.content.End)
}
