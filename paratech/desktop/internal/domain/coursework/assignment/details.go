package assignment

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of the details. The title limit is the one of the extraction answer; the description allows more, because
// the user may write it out.
const (
	MaxTitleRunes       = 200
	MaxDescriptionRunes = 16000
)

// Details is what an assignment says: a one-line title, a description that may be empty and an optional deadline.
// It is a validated value: build it with NewDetails; the zero value is invalid.
type Details struct {
	title       string
	description string
	due         time.Time
}

// NewDetails validates and normalises the details: texts are trimmed, the title is one line of 1 to MaxTitleRunes
// characters, the description at most MaxDescriptionRunes characters without control characters other than line
// breaks and tabs, and a deadline is kept in UTC. A zero due means no deadline.
func NewDetails(title, description string, due time.Time) (Details, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	switch {
	case title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxTitleRunes:
		return Details{}, fmt.Errorf("%w: title must be 1-%d characters of valid text", ErrInvalidDetails, MaxTitleRunes)
	case strings.ContainsFunc(title, notInLine):
		return Details{}, fmt.Errorf("%w: title must be one line without control characters", ErrInvalidDetails)
	case !utf8.ValidString(description) || utf8.RuneCountInString(description) > MaxDescriptionRunes:
		return Details{}, fmt.Errorf("%w: description must be at most %d characters of valid text", ErrInvalidDetails, MaxDescriptionRunes)
	case strings.ContainsFunc(description, badControl):
		return Details{}, fmt.Errorf("%w: description contains control characters", ErrInvalidDetails)
	}
	if !due.IsZero() {
		due = due.UTC()
	}
	return Details{title: title, description: description, due: due}, nil
}

// Title returns the one-line title.
func (d Details) Title() string { return d.title }

// Description returns the description; may be empty.
func (d Details) Description() string { return d.description }

// Due returns the deadline in UTC; zero when there is none.
func (d Details) Due() time.Time { return d.due }

// IsZero reports whether d is the invalid zero value.
func (d Details) IsZero() bool { return d.title == "" }

// Equal reports whether both details say the same: same texts and the same deadline instant.
func (d Details) Equal(o Details) bool {
	return d.title == o.title && d.description == o.description && d.due.Equal(o.due)
}

func badControl(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }

// notInLine also rejects the line and paragraph separators, which break a line in many renderers.
func notInLine(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }
