package summary

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Limits of the sections (TASK-101). They are those of the summary answer (domain/coursework/services/prompts), so
// every valid answer fits. MaxTextRunes bounds the headings, texts and points of all sections together.
const (
	MaxSections         = 20
	MaxHeadingRunes     = 200
	MaxSectionTextRunes = 4000
	MaxSectionPoints    = 10
	MaxPointRunes       = 300
)

// Section is one logical section of a summary: a short heading, its text (1-3 paragraphs) and optional points. Only
// the single section of a summary stored before TASK-101 has no heading. Content validates sections; a Section
// returned by Content is a copy.
type Section struct {
	Heading string
	Text    string
	Points  []string
}

// checkSection validates and trims s as a headed section.
func checkSection(i int, s Section) (Section, error) {
	heading := strings.TrimSpace(s.Heading)
	if heading == "" || !utf8.ValidString(heading) || utf8.RuneCountInString(heading) > MaxHeadingRunes ||
		strings.ContainsFunc(heading, notInLine) {
		return Section{}, fmt.Errorf("%w: section %d heading must be one line of 1-%d characters", ErrInvalidContent, i, MaxHeadingRunes)
	}
	text := strings.TrimSpace(s.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxSectionTextRunes ||
		strings.ContainsFunc(text, badControl) {
		return Section{}, fmt.Errorf("%w: section %d text must be 1-%d characters without control characters",
			ErrInvalidContent, i, MaxSectionTextRunes)
	}
	if len(s.Points) > MaxSectionPoints {
		return Section{}, fmt.Errorf("%w: section %d has more than %d points", ErrInvalidContent, i, MaxSectionPoints)
	}
	var points []string
	for j, p := range s.Points {
		p = strings.TrimSpace(p)
		if p == "" || !utf8.ValidString(p) || utf8.RuneCountInString(p) > MaxPointRunes || strings.ContainsFunc(p, notInLine) {
			return Section{}, fmt.Errorf("%w: section %d point %d must be one line of 1-%d characters",
				ErrInvalidContent, i, j, MaxPointRunes)
		}
		points = append(points, p)
	}
	return Section{Heading: heading, Text: text, Points: points}, nil
}

func (s Section) runes() int {
	n := utf8.RuneCountInString(s.Heading) + utf8.RuneCountInString(s.Text)
	for _, p := range s.Points {
		n += utf8.RuneCountInString(p)
	}
	return n
}

func (s Section) clone() Section {
	s.Points = slices.Clone(s.Points)
	return s
}

func (s Section) equal(o Section) bool {
	return s.Heading == o.Heading && s.Text == o.Text && slices.Equal(s.Points, o.Points)
}

// plainText renders headed sections as plain text: per section the heading with a colon, its text and its points
// as "- " lines; sections are separated by an empty line.
func plainText(sections []Section) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimRight(s.Heading, ":") + ":\n" + s.Text)
		for _, p := range s.Points {
			b.WriteString("\n- " + p)
		}
	}
	return b.String()
}
