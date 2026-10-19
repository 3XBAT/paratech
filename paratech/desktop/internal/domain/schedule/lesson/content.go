package lesson

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of a lesson.
const (
	MaxTitleBytes     = 512
	MaxTimeZoneBytes  = 64
	MaxSourceUIDBytes = 512
	// MaxDuration catches typos such as a wrong end date; no class lasts longer.
	MaxDuration = 24 * time.Hour
)

// Content is what a lesson shows: the part an import compares and the calendar projection displays. It is a value:
// compare it with Equal, not ==, because times with the same instant may differ in location.
type Content struct {
	Title    string
	Start    time.Time
	End      time.Time
	TimeZone string // IANA name of the source record, e.g. Europe/Moscow

	// Subject is the course the class belongs to, "" when unknown (a lesson stored before subjects existed and
	// never imported again). Kind is the type of the class. Teacher is optional. See ClassifyTitle.
	Subject string
	Kind    Kind
	Teacher string
}

// Validate checks the invariants of a lesson's content.
func (c Content) Validate() error {
	if err := ValidateTitle(c.Title); err != nil {
		return err
	}
	if err := ValidateSpan(c.Start, c.End); err != nil {
		return err
	}
	if err := ValidateTimeZoneName(c.TimeZone); err != nil {
		return err
	}
	if err := ValidateSubject(c.Subject); err != nil {
		return err
	}
	if err := c.Kind.Validate(); err != nil {
		return err
	}
	return ValidateTeacher(c.Teacher)
}

// ValidateTitle checks a lesson title: not blank, at most MaxTitleBytes, valid text without control characters.
// Parsers call it to name the column of a bad title.
func ValidateTitle(title string) error {
	switch {
	case strings.TrimSpace(title) == "":
		return fmt.Errorf("%w: empty title", ErrInvalidLesson)
	case len(title) > MaxTitleBytes:
		return fmt.Errorf("%w: title longer than %d bytes", ErrInvalidLesson, MaxTitleBytes)
	case !utf8.ValidString(title) || strings.ContainsFunc(title, unicode.IsControl):
		return fmt.Errorf("%w: title is not valid text", ErrInvalidLesson)
	}
	return nil
}

// ValidateSpan checks the time range of a lesson: both ends set, end after start, at most MaxDuration.
func ValidateSpan(start, end time.Time) error {
	switch {
	case start.IsZero() || end.IsZero():
		return fmt.Errorf("%w: missing start or end", ErrInvalidLesson)
	case !end.After(start):
		return fmt.Errorf("%w: end is not after start", ErrInvalidLesson)
	case end.Sub(start) > MaxDuration:
		return fmt.Errorf("%w: longer than %v", ErrInvalidLesson, MaxDuration)
	}
	return nil
}

// Equal reports whether both contents show the same lesson: same texts, same instants, same zone name and kind.
func (c Content) Equal(o Content) bool {
	return c.Title == o.Title && c.Start.Equal(o.Start) && c.End.Equal(o.End) && c.TimeZone == o.TimeZone &&
		c.Subject == o.Subject && c.Kind == o.Kind && c.Teacher == o.Teacher
}

// Overlaps reports whether the two time ranges share time. Touching ranges (one ends when the other starts) do not.
func (c Content) Overlaps(o Content) bool {
	return c.Start.Before(o.End) && o.Start.Before(c.End)
}

// ValidateTimeZoneName checks the form of an IANA zone name ("UTC", "Europe/Moscow", "Etc/GMT+3"). Whether the zone
// exists is checked by whoever loads zone data (a parser), not by the domain.
func ValidateTimeZoneName(name string) error {
	if name == "" || len(name) > MaxTimeZoneBytes {
		return fmt.Errorf("%w: time zone name %q", ErrInvalidLesson, name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%w: time zone name %q", ErrInvalidLesson, name)
		}
	}
	for _, r := range name {
		ok := r == '/' || r == '_' || r == '-' || r == '+' ||
			('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
		if !ok {
			return fmt.Errorf("%w: time zone name %q", ErrInvalidLesson, name)
		}
	}
	return nil
}

// ValidateSourceUID checks the identity of an imported entry; the empty uid (a manual lesson) is valid.
func ValidateSourceUID(uid string) error {
	switch {
	case len(uid) > MaxSourceUIDBytes:
		return fmt.Errorf("%w: source uid longer than %d bytes", ErrInvalidLesson, MaxSourceUIDBytes)
	case !utf8.ValidString(uid) || strings.ContainsFunc(uid, unicode.IsControl):
		return fmt.Errorf("%w: source uid is not valid text", ErrInvalidLesson)
	}
	return nil
}
