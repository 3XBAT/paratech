// Package scheduler is the use case that watches the local schedule and announces that a lesson is starting
// (PRD section 6.1, AC-01): a configurable lead time before the start it publishes lesson.StartAnnounced, which the
// recording prompt (TASK-022) consumes. Whether prompts are enabled (AC-02) is checked by that consumer.
//
// Run polls on the Clock and Sleeper ports; every Tick lists the lessons that may be running or about to start and
// lets the lesson aggregate decide (StartAnnouncementDue, AnnounceStart). The mark is saved before the event is
// published, so a restart never repeats an announcement (at most once: an event lost by a crash between the two is
// not raised again). The window is a pair of instants, so the zone of the record and daylight-saving changes are
// respected as stored by the import.
//
// Rules that apply: ARCH-20..27.
package scheduler
