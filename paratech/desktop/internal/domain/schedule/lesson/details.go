package lesson

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of the optional text of a lesson, in characters.
const (
	MaxSubjectRunes = 200
	MaxTeacherRunes = 200
)

// CleanText turns untrusted text from a schedule file into one line of at most max characters: invalid UTF-8 and
// control, line separator and bidi formatting characters are dropped or become spaces, spaces are collapsed.
func CleanText(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case isBidiControl(r):
		case unicode.IsControl(r), r == '\u2028', r == '\u2029':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > max {
		out = strings.TrimSpace(string([]rune(out)[:max]))
	}
	return out
}

// isBidiControl reports the bidirectional formatting characters: LRM, RLM, ALM, the embeddings, overrides and isolates.
func isBidiControl(r rune) bool {
	return r == '\u200e' || r == '\u200f' || r == '\u061c' || (r >= '\u202a' && r <= '\u202e') ||
		(r >= '\u2066' && r <= '\u2069')
}

// ValidateSubject checks the subject of a lesson: optional, CleanText of itself within MaxSubjectRunes.
func ValidateSubject(s string) error { return validateLine("subject", s, MaxSubjectRunes) }

// ValidateTeacher checks the teacher of a lesson: optional, CleanText of itself within MaxTeacherRunes.
func ValidateTeacher(s string) error { return validateLine("teacher", s, MaxTeacherRunes) }

func validateLine(name, s string, max int) error {
	if s != CleanText(s, max) {
		return fmt.Errorf("%w: %s is not one clean line of at most %d characters", ErrInvalidLesson, name, max)
	}
	return nil
}
