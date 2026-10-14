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
}

// Validate checks the invariants of a lesson's content.
func (c Content) Validate() error {
	switch {
	case strings.TrimSpace(c.Title) == "":
		return fmt.Errorf("%w: empty title", ErrInvalidLesson)
	case len(c.Title) > MaxTitleBytes:
		return fmt.Errorf("%w: title longer than %d bytes", ErrInvalidLesson, MaxTitleBytes)
	case !utf8.ValidString(c.Title) || strings.ContainsFunc(c.Title, unicode.IsControl):
		return fmt.Errorf("%w: title is not valid text", ErrInvalidLesson)
	case c.Start.IsZero() || c.End.IsZero():
		return fmt.Errorf("%w: missing start or end", ErrInvalidLesson)
	case !c.End.After(c.Start):
		return fmt.Errorf("%w: end is not after start", ErrInvalidLesson)
	case c.End.Sub(c.Start) > MaxDuration:
		return fmt.Errorf("%w: longer than %v", ErrInvalidLesson, MaxDuration)
	}
	return ValidateTimeZoneName(c.TimeZone)
}

// Equal reports whether both contents show the same lesson: same title, same instants, same zone name.
func (c Content) Equal(o Content) bool {
	return c.Title == o.Title && c.Start.Equal(o.Start) && c.End.Equal(o.End) && c.TimeZone == o.TimeZone
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
