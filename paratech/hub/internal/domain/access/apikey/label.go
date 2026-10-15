package apikey

import "fmt"

// MaxLabelLen bounds a label.
const MaxLabelLen = 32

// Label is the short pseudonymous name of the tester a key was issued to, for example "tester-07" (PRD section 13.2:
// minimal pseudonymous identifiers). The charset leaves no room for names, emails or free text.
type Label struct {
	v string
}

// NewLabel validates a label: 1 to 32 characters of a-z, 0-9, '-' and '_', starting with a letter or digit.
func NewLabel(s string) (Label, error) {
	if s == "" || len(s) > MaxLabelLen {
		return Label{}, fmt.Errorf("%w: must be 1 to %d characters", ErrInvalidLabel, MaxLabelLen)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if !alnum && (i == 0 || (c != '-' && c != '_')) {
			return Label{}, fmt.Errorf("%w: use a-z, 0-9, '-' and '_', starting with a letter or digit", ErrInvalidLabel)
		}
	}
	return Label{v: s}, nil
}

// String returns the label text.
func (l Label) String() string { return l.v }

// IsZero reports whether the label was not built by NewLabel.
func (l Label) IsZero() bool { return l.v == "" }
