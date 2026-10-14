package lesson

import (
	"fmt"
	"time"
)

// MaxStartLead bounds how early the "lesson is starting" prompt may come: earlier than this it is no longer "starting".
const MaxStartLead = time.Hour

// StartLateGrace is how long after the start the prompt may still come (owner decision 2026-10-14): a lesson that
// started earlier is already under way and is not offered for recording. The grace only absorbs a late poll, so a
// scheduler must poll more often than this.
const StartLateGrace = 20 * time.Second

// ValidateStartLead checks the lead time of the start announcement: from zero (at the start) up to MaxStartLead.
func ValidateStartLead(lead time.Duration) error {
	if lead < 0 || lead > MaxStartLead {
		return fmt.Errorf("%w: %v, want 0..%v", ErrInvalidLead, lead, MaxStartLead)
	}
	return nil
}

// StartAnnouncementDue reports whether the "lesson is starting" prompt should be raised at now (PRD section 6.1):
// the lesson is live, its prompt was not raised yet, and now lies in [start-lead, start+StartLateGrace). An app
// started in the middle of a class does not offer to record it. Times are compared as instants, so the zone of the
// record and daylight-saving changes cannot shift the window.
func (l *Lesson) StartAnnouncementDue(now time.Time, lead time.Duration) bool {
	if l.Deleted() || !l.startNotifiedAt.IsZero() || now.IsZero() || ValidateStartLead(lead) != nil {
		return false
	}
	return !now.Before(l.content.Start.Add(-lead)) && now.Before(l.content.Start.Add(StartLateGrace))
}

// AnnounceStart records that the "lesson is starting" prompt is raised at now and returns the event to publish.
// It succeeds at most once per start time: a restarted scheduler sees StartNotifiedAt and stays silent, and only
// moving the start (ChangeContent) makes the lesson due again.
func (l *Lesson) AnnounceStart(now time.Time, lead time.Duration) (StartAnnounced, error) {
	if err := ValidateStartLead(lead); err != nil {
		return StartAnnounced{}, err
	}
	switch {
	case now.IsZero():
		return StartAnnounced{}, fmt.Errorf("%w: no announcement time", ErrInvalidLesson)
	case l.Deleted():
		return StartAnnounced{}, ErrDeleted
	case !l.startNotifiedAt.IsZero():
		return StartAnnounced{}, ErrAlreadyAnnounced
	case !l.StartAnnouncementDue(now, lead):
		return StartAnnounced{}, fmt.Errorf("%w: %v is outside [%v, %v)", ErrNotDue, now,
			l.content.Start.Add(-lead), l.content.Start.Add(StartLateGrace))
	}

	l.startNotifiedAt = now
	l.updatedAt = now
	return StartAnnounced{
		LessonID: l.id, Title: l.content.Title, Start: l.content.Start, End: l.content.End,
		TimeZone: l.content.TimeZone, AnnouncedAt: now,
	}, nil
}
