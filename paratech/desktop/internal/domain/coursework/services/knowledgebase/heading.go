package knowledgebase

import "strings"

// MaxHeadingRunes bounds the heading of a section of a note a hit or a passage keeps; it is the bound of an answer's
// source (answer.MaxHeadingRunes), so the label of a hit and of a citation read the same.
const MaxHeadingRunes = 120

// fragmentHeading is the heading of a note fragment of a section (headingOf), "" for the other locators.
func fragmentHeading(f NoteFragment) string {
	if f.Locator.Kind != locatorSection {
		return ""
	}
	return headingOf(f.Text)
}

// cutHeading cuts a heading to MaxHeadingRunes, the last of them "…".
func cutHeading(s string) string {
	if r := []rune(s); len(r) > MaxHeadingRunes {
		return strings.TrimSpace(string(r[:MaxHeadingRunes-1])) + ellipsis
	}
	return s
}
