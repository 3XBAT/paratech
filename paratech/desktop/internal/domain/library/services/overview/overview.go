// Package overview holds the rules of the page of a subject (TASK-225): its nearest deadlines, the schedule line of
// its lessons, the status of its recorded lessons and the «новое» mark of what appeared since the page was last
// opened. Titles and names are data: nothing here reads them.
package overview

import (
	"cmp"
	"slices"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Limits of the page.
const (
	MaxDeadlines = 5  // «Ближайшее»
	MaxLessons   = 10 // the latest recorded lessons
	MaxSlots     = 14 // the schedule line
)

// Deadline is an assignment of the subject with its deadline. Title is model output or the student's text, shown as
// data. DaysLeft counts calendar days from today in the student's zone (0 is today).
type Deadline struct {
	Assignment shared.AssignmentID
	Title      string
	Due        time.Time // zero: no deadline
	DaysLeft   int
}

// Nearest returns the deadlines not yet passed, soonest first, at most MaxDeadlines, with DaysLeft set; days are read
// in now's location.
func Nearest(ds []Deadline, now time.Time) []Deadline {
	out := []Deadline{}
	for _, d := range ds {
		if d.Due.IsZero() || d.Due.Before(now) {
			continue
		}
		d.DaysLeft = daysFrom(now, d.Due)
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b Deadline) int {
		return cmp.Or(a.Due.Compare(b.Due), cmp.Compare(a.Assignment, b.Assignment))
	})
	if len(out) > MaxDeadlines {
		out = out[:MaxDeadlines]
	}
	return out
}

// daysFrom returns the calendar days from the day of now to the day of t, both read in now's location.
func daysFrom(now, t time.Time) int {
	day := func(x time.Time) time.Time {
		y, m, d := x.In(now.Location()).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(day(t).Sub(day(now)) / (24 * time.Hour))
}

// Scheduled is a lesson of the subject in the imported schedule. Kind is a lesson kind code ("" or "unknown" when the
// schedule does not say).
type Scheduled struct {
	Kind  string
	Start time.Time
	End   time.Time
}

// Slot is a weekly place of the subject's lessons: «Лекции — вт, 10:30». StartMinute and EndMinute count minutes
// from midnight in the student's zone; Weekday is read there too.
type Slot struct {
	Kind        string
	Weekday     time.Weekday
	StartMinute int
	EndMinute   int
}

// Slots returns the distinct weekly slots of the lessons read in loc, Monday first, then by start, end and kind; at
// most MaxSlots.
func Slots(lessons []Scheduled, loc *time.Location) []Slot {
	out := []Slot{}
	for _, l := range lessons {
		start, end := l.Start.In(loc), l.End.In(loc)
		s := Slot{Kind: l.Kind, Weekday: start.Weekday(), StartMinute: minuteOf(start), EndMinute: minuteOf(end)}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Slot) int {
		return cmp.Or(cmp.Compare(mondayFirst(a.Weekday), mondayFirst(b.Weekday)), cmp.Compare(a.StartMinute, b.StartMinute),
			cmp.Compare(a.EndMinute, b.EndMinute), cmp.Compare(a.Kind, b.Kind))
	})
	if len(out) > MaxSlots {
		out = out[:MaxSlots]
	}
	return out
}

func minuteOf(t time.Time) int { return t.Hour()*60 + t.Minute() }

// mondayFirst numbers the weekdays from Monday (0) to Sunday (6).
func mondayFirst(d time.Weekday) int { return (int(d) + 6) % 7 }

// Status is the status chip of a recorded lesson.
type Status string

// Statuses, from the least to the most done; StatusFailed is a failed step.
const (
	StatusRecorded     Status = "recorded"     // the recording is there, not transcribed yet
	StatusTranscribed  Status = "transcribed"  // the transcript is there, no summary yet
	StatusSummaryReady Status = "summaryReady" // the summary is ready
	StatusFailed       Status = "failed"       // its processing failed and no summary is there
)

// Progress is what is known of the processing of a recorded lesson.
type Progress struct {
	Transcribed bool // its newest transcript is stored
	Summary     bool // that transcript has a summary
	Failed      bool // the last step of its processing failed
}

// StatusOf returns the status of a recorded lesson: a ready summary wins over a failure of a later step (the lecture
// text), a failure over the progress before it.
func StatusOf(p Progress) Status {
	switch {
	case p.Summary && p.Transcribed:
		return StatusSummaryReady
	case p.Failed:
		return StatusFailed
	case p.Transcribed:
		return StatusTranscribed
	}
	return StatusRecorded
}

// Fresh reports whether something changed at changed is «новое» for a page last opened at opened: only after an
// opening, and only what changed after it. A page never opened marks nothing.
func Fresh(changed, opened time.Time) bool {
	return !opened.IsZero() && changed.After(opened)
}

// FreshMaterials returns the active materials added to the subject or changed after opened, in their order.
func FreshMaterials(ms []*material.Material, opened time.Time) []*material.Material {
	out := []*material.Material{}
	for _, m := range ms {
		if m.Active() && Fresh(m.UpdatedAt(), opened) {
			out = append(out, m)
		}
	}
	return out
}
