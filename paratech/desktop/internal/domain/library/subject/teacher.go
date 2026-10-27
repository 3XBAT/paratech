package subject

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of a teacher.
const (
	// MaxTeacherNameRunes bounds the name of a teacher.
	MaxTeacherNameRunes = 200

	// MaxTeacherEmailLen bounds the address of a teacher, in bytes (RFC 5321 path limit).
	MaxTeacherEmailLen = 254
)

// Teacher is the teacher of a subject the student typed once (ADR-0030): a name and an email address. It is personal
// data: errors name the rule that failed, never the text. The zero value is no teacher.
type Teacher struct {
	name  string
	email string
}

// NewTeacher trims both texts and checks them. The name is one line of 1 to MaxTeacherNameRunes characters of valid
// UTF-8 without control, format (bidi, zero-width) or line separator characters. The address follows the syntax of
// proposal.NewRecipient (a copy, ARCH-11): a plain ASCII "local@domain" without display name, spaces or special
// characters, a dot in the domain, no leading, trailing or double dot. Errors wrap ErrInvalidTeacherName or
// ErrInvalidTeacherEmail (both ErrInvalidTeacher).
func NewTeacher(name, email string) (Teacher, error) {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if err := checkTeacherName(name); err != nil {
		return Teacher{}, err
	}
	if err := checkTeacherEmail(email); err != nil {
		return Teacher{}, err
	}
	return Teacher{name: name, email: email}, nil
}

func checkTeacherName(s string) error {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s):
		return fmt.Errorf("%w: not valid UTF-8", ErrInvalidTeacherName)
	case n == 0:
		return fmt.Errorf("%w: empty", ErrInvalidTeacherName)
	case n > MaxTeacherNameRunes:
		return fmt.Errorf("%w: longer than %d characters", ErrInvalidTeacherName, MaxTeacherNameRunes)
	case strings.ContainsFunc(s, notInTeacherName):
		return fmt.Errorf("%w: must be one line without control characters", ErrInvalidTeacherName)
	}
	return nil
}

func notInTeacherName(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

func checkTeacherEmail(s string) error {
	if len(s) < 3 || len(s) > MaxTeacherEmailLen {
		return fmt.Errorf("%w: length must be 3-%d", ErrInvalidTeacherEmail, MaxTeacherEmailLen)
	}
	if strings.ContainsFunc(s, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune(`<>(),;:"[]\`, r)
	}) {
		return fmt.Errorf("%w: forbidden character", ErrInvalidTeacherEmail)
	}
	local, domain, ok := strings.Cut(s, "@")
	switch {
	case !ok || strings.Contains(domain, "@"):
		return fmt.Errorf("%w: want exactly one @", ErrInvalidTeacherEmail)
	case !dotAtom(local):
		return fmt.Errorf("%w: malformed local part", ErrInvalidTeacherEmail)
	case !strings.Contains(domain, ".") || !dotAtom(domain):
		return fmt.Errorf("%w: malformed domain", ErrInvalidTeacherEmail)
	}
	return nil
}

// dotAtom reports whether s is non-empty and has no leading, trailing or double dot.
func dotAtom(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".") && !strings.Contains(s, "..")
}

// Name returns the name of the teacher; "" for no teacher.
func (t Teacher) Name() string { return t.name }

// Email returns the address of the teacher; "" for no teacher.
func (t Teacher) Email() string { return t.email }

// IsZero reports whether no teacher is set.
func (t Teacher) IsZero() bool { return t == Teacher{} }

// String hides the personal data, so a teacher printed by mistake (%v, a log attribute) shows nothing of it.
func (t Teacher) String() string {
	if t.IsZero() {
		return "no teacher"
	}
	return "teacher"
}
