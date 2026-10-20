package values

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrInvalidCorrection is returned by NewLessonCorrection for a subject or kind the user cannot set.
var ErrInvalidCorrection = errors.New("invalid lesson correction")

// lessonKindCodes are the kinds the user can choose; the schedule uses the same codes (domain/schedule/lesson.Kind).
var lessonKindCodes = []string{"lecture", "seminar", "practice", "lab"}

// LessonKindCodes returns the kind codes a correction accepts.
func LessonKindCodes() []string { return slices.Clone(lessonKindCodes) }

// LessonCorrection is the subject and the kind of the lesson of a recording as the user set them (TASK-091). It wins
// over the lesson snapshot, which stays as recorded. The zero value corrects nothing; an empty field corrects nothing
// either, so the snapshot or the schedule names it again.
type LessonCorrection struct {
	subject string
	kind    string
}

// NewLessonCorrection checks what the user typed: the subject becomes one plain line (as a snapshot cleans it) of at
// most MaxLessonSubjectBytes; the kind is "" or one of LessonKindCodes, in any letter case.
func NewLessonCorrection(subject, kind string) (LessonCorrection, error) {
	clean := cleanLine(subject, len(subject)) // cleaning never makes it longer, so nothing is cut here
	if len(clean) > MaxLessonSubjectBytes {
		return LessonCorrection{}, fmt.Errorf("%w: subject longer than %d bytes", ErrInvalidCorrection, MaxLessonSubjectBytes)
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && !slices.Contains(lessonKindCodes, kind) {
		return LessonCorrection{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidCorrection, kind)
	}
	return LessonCorrection{subject: clean, kind: kind}, nil
}

// Subject returns the subject the user set; empty if none.
func (c LessonCorrection) Subject() string { return c.subject }

// Kind returns the kind code the user set; empty if none.
func (c LessonCorrection) Kind() string { return c.kind }

// IsZero reports that nothing is corrected.
func (c LessonCorrection) IsZero() bool { return c == LessonCorrection{} }
