package assignment

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// Limits of an assignment the student adds (ADR-0029). The title limit is MaxTitleRunes.
const (
	MaxNoteRunes = 2000

	dueBackDays   = 31 // a deadline may lie this many days before now
	dueAheadYears = 2  // and at most this many years after it

	// A date without a time of day is due at the end of that day (as the agent's edit_due).
	endOfDayHour   = 23
	endOfDayMinute = 59

	dateLayout = "2006-01-02"
)

// StudentSpec is what the student provides to NewByStudent (ADR-0029). Title, Note and SubjectName are the student's
// own words: data, never instructions.
type StudentSpec struct {
	ID          shared.AssignmentID
	Lesson      shared.LessonID // optional
	Title       string
	Note        string    // optional; stored as the description
	Due         time.Time // zero: no deadline
	SubjectName string    // optional: the name of the library subject, stored as the student's correction
}

// NewByStudent creates an assignment the student added by hand or through the agent, never stored (Version() == 0).
// It is not extracted (source -1, no prompt version, no transcript or material) and counts as user-edited, so
// reprocessing never touches it. The subject is the student's correction; the teacher stays empty. Besides the rules
// of NewDetails the title must hold no bidi controls, the note at most MaxNoteRunes characters without control
// characters other than line breaks and without bidi controls, and a deadline must lie within now - 31 days ...
// now + 2 years. Violations wrap ErrInvalidTitle, ErrInvalidNote or ErrInvalidDue (all ErrInvalidDetails).
func NewByStudent(spec StudentSpec, now time.Time) (*Assignment, error) {
	if err := checkStudentTitle(spec.Title); err != nil {
		return nil, err
	}
	if err := checkStudentNote(spec.Note); err != nil {
		return nil, err
	}
	if !spec.Due.IsZero() && (spec.Due.Before(now.AddDate(0, 0, -dueBackDays)) || spec.Due.After(now.AddDate(dueAheadYears, 0, 0))) {
		return nil, fmt.Errorf("%w: outside %d days back ... %d years ahead", ErrInvalidDue, dueBackDays, dueAheadYears)
	}
	d, err := NewDetails(spec.Title, spec.Note, spec.Due)
	if err != nil {
		return nil, err
	}

	a := &Assignment{
		id: spec.ID, lesson: spec.Lesson, source: noSource, details: d, userEdited: true,
		correction: values.NewLessonNames(spec.SubjectName, ""), createdAt: now, updatedAt: now,
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return a, nil
}

// StudentDue is the deadline the student named: date (YYYY-MM-DD) at timeOfDay (HH:MM, 24 h) in loc, or 23:59 of that
// day without a time. No date is no deadline (zero); a time without a date wraps ErrInvalidTime, a malformed date
// ErrInvalidDue.
func StudentDue(date, timeOfDay string, loc *time.Location) (time.Time, error) {
	switch {
	case date == "" && timeOfDay != "":
		return time.Time{}, fmt.Errorf("%w: a time without a date", ErrInvalidTime)
	case date == "":
		return time.Time{}, nil
	case loc == nil:
		return time.Time{}, fmt.Errorf("%w: no time zone", ErrInvalidDue)
	}
	day, err := time.Parse(dateLayout, date)
	if err != nil || len(date) != len(dateLayout) {
		return time.Time{}, fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalidDue)
	}
	h, m := endOfDayHour, endOfDayMinute
	if timeOfDay != "" {
		var ok bool
		if h, m, ok = parseClock(timeOfDay); !ok {
			return time.Time{}, fmt.Errorf("%w: time must be HH:MM", ErrInvalidTime)
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, loc), nil
}

// ByStudent reports whether the student added the assignment (NewByStudent): it was not extracted.
func (a *Assignment) ByStudent() bool { return a.source == noSource }

// SameByStudent reports whether a and o are both assignments the student added with the same lesson, details and
// subject, so that adding o again is a repeat of a (ADR-0029: idempotency by the caller's ID). A deleted one is never
// repeated: adding it again would bring it back.
func (a *Assignment) SameByStudent(o *Assignment) bool {
	return a.ByStudent() && o.ByStudent() && !a.Deleted() && !o.Deleted() && a.lesson == o.lesson && a.details.Equal(o.details) &&
		a.correction == o.correction
}

func checkStudentTitle(title string) error {
	title = strings.TrimSpace(title)
	switch {
	case title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxTitleRunes:
		return fmt.Errorf("%w: must be 1-%d characters of valid text", ErrInvalidTitle, MaxTitleRunes)
	case strings.ContainsFunc(title, func(r rune) bool { return notInLine(r) || bidiControl(r) }):
		return fmt.Errorf("%w: must be one line without control characters", ErrInvalidTitle)
	}
	return nil
}

func checkStudentNote(note string) error {
	note = strings.TrimSpace(note)
	switch {
	case !utf8.ValidString(note) || utf8.RuneCountInString(note) > MaxNoteRunes:
		return fmt.Errorf("%w: must be at most %d characters of valid text", ErrInvalidNote, MaxNoteRunes)
	case strings.ContainsFunc(note, func(r rune) bool { return controlNotBreak(r) || bidiControl(r) }):
		return fmt.Errorf("%w: contains control characters", ErrInvalidNote)
	}
	return nil
}

// controlNotBreak reports a control character other than a line break.
func controlNotBreak(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' }

// bidiControl reports the embedding, override and isolate characters, which can reorder how a text reads.
func bidiControl(r rune) bool { return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) }

// parseClock reads HH:MM, 24 h, exactly two digits each.
func parseClock(s string) (h, m int, ok bool) {
	if len(s) != len("15:04") || s[2] != ':' {
		return 0, 0, false
	}
	digits := []byte{s[0], s[1], s[3], s[4]}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, 0, false
		}
	}
	h, m = int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0')
	return h, m, h <= endOfDayHour && m <= endOfDayMinute
}
