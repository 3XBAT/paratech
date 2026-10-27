package values

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of mentions (ADR-0028 «References, not copies»).
const (
	MaxMentions          = 8   // per request
	MaxMentionLabelRunes = 200 // the name shown at mention time
)

// MentionKinds returns the kinds of objects a student can mention with «@».
func MentionKinds() []IDKind {
	return []IDKind{KindSubject, KindLesson, KindMaterial, KindAssignment}
}

// Mention is an object the student mentioned in a request: its typed ID and the name shown at mention time. The label
// is for display only; it never enters the history or a Hub request (ADR-0022), only the ID does.
type Mention struct {
	ref   Ref
	label string
}

// NewMention checks the kind of the ref and the label: one line of 1..200 characters after trimming, no control or
// format characters (bidi overrides, zero-width).
func NewMention(ref Ref, label string) (Mention, error) {
	if ref.IsZero() || !slices.Contains(MentionKinds(), ref.Kind()) {
		return Mention{}, fmt.Errorf("%w: a mention is a subject, lesson, material or assignment", ErrInvalidMention)
	}
	label = strings.TrimSpace(label)
	if label == "" || !utf8.ValidString(label) || utf8.RuneCountInString(label) > MaxMentionLabelRunes {
		return Mention{}, fmt.Errorf("%w: a label has 1..%d characters", ErrInvalidMention, MaxMentionLabelRunes)
	}
	for _, r := range label {
		// IsGraphic keeps ordinary spaces (U+00A0 from copied names) and refuses Cc, Cf, line and paragraph separators.
		if r == utf8.RuneError || !unicode.IsGraphic(r) {
			return Mention{}, fmt.Errorf("%w: a label is one line without control or format characters", ErrInvalidMention)
		}
	}
	return Mention{ref: ref, label: label}, nil
}

// Ref returns the ID of the mentioned object.
func (m Mention) Ref() Ref { return m.ref }

// Label returns the name shown at mention time, for display only.
func (m Mention) Label() string { return m.label }

// IsZero reports whether m mentions nothing.
func (m Mention) IsZero() bool { return m == Mention{} }

// CheckMentions checks the mentions of one request: each built by NewMention, distinct by ID, at most MaxMentions.
func CheckMentions(ms []Mention) error {
	if len(ms) > MaxMentions {
		return fmt.Errorf("%w: at most %d mentions per request", ErrInvalidMention, MaxMentions)
	}
	seen := RefSet{}
	for _, m := range ms {
		if _, err := NewMention(m.ref, m.label); err != nil {
			return err
		}
		if seen.Has(m.ref) {
			return fmt.Errorf("%w: an object is mentioned twice", ErrInvalidMention)
		}
		seen.Add(m.ref)
	}
	return nil
}

// MentionRefs returns the IDs of the mentions, the only part that goes to the planner.
func MentionRefs(ms []Mention) []Ref {
	out := make([]Ref, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ref)
	}
	return out
}
