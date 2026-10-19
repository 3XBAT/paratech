package settings

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxProfileRunes bounds each field of the student profile.
const MaxProfileRunes = 200

// ProfileFields are the texts of a student profile as the user typed them; every field is optional.
type ProfileFields struct {
	FullName   string // «Иванов Иван Иванович»
	Group      string // «ИВТ-21»
	University string
	Faculty    string // faculty or department
	City       string
}

// Profile is the student profile (TASK-094): what the title page of a homework document names. Local data, not a
// secret. Build it with NewProfile; the zero value names nothing.
type Profile struct {
	fields ProfileFields
}

// NewProfile trims every field and checks it: one line of at most MaxProfileRunes characters of valid UTF-8, without
// control, format (bidi, zero-width) or line separator characters. Empty fields are valid. Errors wrap
// ErrInvalidSettings and name the field, never its text.
func NewProfile(f ProfileFields) (Profile, error) {
	fields := []struct {
		name string
		v    *string
	}{
		{"full name", &f.FullName}, {"group", &f.Group}, {"university", &f.University}, {"faculty", &f.Faculty},
		{"city", &f.City},
	}
	for _, fl := range fields {
		*fl.v = strings.TrimSpace(*fl.v)
		if err := checkProfileText(*fl.v); err != nil {
			return Profile{}, fmt.Errorf("%w: profile %s %s", ErrInvalidSettings, fl.name, err)
		}
	}
	return Profile{fields: f}, nil
}

func checkProfileText(s string) error {
	switch {
	case !utf8.ValidString(s):
		return errors.New("is not valid UTF-8")
	case utf8.RuneCountInString(s) > MaxProfileRunes:
		return fmt.Errorf("is longer than %d characters", MaxProfileRunes)
	case strings.ContainsFunc(s, notInProfile):
		return errors.New("must be one line without control characters")
	}
	return nil
}

func notInProfile(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

// Fields returns the texts of the profile.
func (p Profile) Fields() ProfileFields { return p.fields }

// IsZero reports whether the profile names nothing.
func (p Profile) IsZero() bool { return p == Profile{} }

// Profile returns the student profile; the zero Profile while none is set.
func (s *Settings) Profile() Profile { return s.profile }

// SetProfile replaces the student profile (TASK-094). Setting the same profile is a no-op.
func (s *Settings) SetProfile(p Profile, now time.Time) error {
	return change(s, &s.profile, p, now)
}
