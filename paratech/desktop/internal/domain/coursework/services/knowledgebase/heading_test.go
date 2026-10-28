package knowledgebase

import (
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/answer"
)

// Characters a heading must not keep, built from code points so the source shows no invisible text.
var (
	headingRLO  = string(rune(0x202e)) // right-to-left override
	headingZWSP = string(rune(0x200b)) // zero-width space
	headingLSEP = string(rune(0x2028)) // line separator
)

// A hit's heading is what an answer's source may keep, so the two labels read the same.
func TestMaxHeadingRunesIsTheSourceBound(t *testing.T) {
	if MaxHeadingRunes != answer.MaxHeadingRunes {
		t.Fatalf("MaxHeadingRunes = %d, answer.MaxHeadingRunes = %d", MaxHeadingRunes, answer.MaxHeadingRunes)
	}
}

func TestSearchHitHeading(t *testing.T) {
	long := strings.Repeat("я", 200)
	section := Locator{Kind: "section", N: 1}
	tests := []struct {
		name  string
		loc   Locator
		text  string
		query string
		want  string
	}{
		{"match in the heading", section, "Ряды\nсходимость", "ряды", "Ряды"},
		{"match in the body", Locator{Kind: "section", N: 2}, "Ряды\nРяд Тейлора сходится", "тейлор", "Ряды"},
		{"heading cut", section, long + "\nтейлор", "тейлор", strings.Repeat("я", MaxHeadingRunes-1) + "…"},
		{"cut drops the space before …", section, strings.Repeat("я", MaxHeadingRunes-2) + " " + long + "\nтейлор",
			"тейлор", strings.Repeat("я", MaxHeadingRunes-2) + "…"},
		{"bidi and zero-width removed", section, "Ря" + headingRLO + "ды" + headingZWSP + " Тейлора\nтейлор", "тейлор", "Ряды Тейлора"},
		{"control characters as one space", section, "\tРяды\x01\x02 Тейлора\r\nтейлор", "тейлор", "Ряды Тейлора"},
		{"line separator ends no line", section, "Ряды" + headingLSEP + "Тейлора\nтейлор", "тейлор", "Ряды Тейлора"},
		{"markup stays text", section, "<b>x</b> [[y]]\nтейлор", "тейлор", "<b>x</b> [[y]]"},
		{"page has none", Locator{Kind: "page", N: 3}, "Ряды\nтейлор", "тейлор", ""},
		{"slide has none", Locator{Kind: "slide", N: 1}, "Ряды\nтейлор", "тейлор", ""},
		{"fragment has none", Locator{Kind: "fragment", N: 1}, "Ряды\nтейлор", "тейлор", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mustSearch(t, tt.query)
			s.Scan(Document{Key: "note:n1", Notes: []NoteFragment{{Note: "n1", Material: "m1", Locator: tt.loc, Text: tt.text}}})
			hits := s.Hits()
			if len(hits) != 1 {
				t.Fatalf("hits = %+v", hits)
			}
			h := hits[0]
			if h.Heading != tt.want || h.Locator != tt.loc || h.Note != "n1" || h.Material != "m1" {
				t.Fatalf("hit = %+v, want heading %q", h, tt.want)
			}
			if n := utf8.RuneCountInString(h.Heading); n > MaxHeadingRunes || strings.ContainsAny(h.Heading, "\n\r") {
				t.Fatalf("heading %q: %d runes or not one line", h.Heading, n)
			}
		})
	}
}

// The other sections of a lesson carry no heading, a section's heading stays with its hit only.
func TestSearchHitHeadingOtherSections(t *testing.T) {
	s := mustSearch(t, "тейлор")
	s.Scan(Document{
		Key: "recording:r1", Recording: "r1", Title: "Тейлор",
		Summary:  []string{"Ряд Тейлора"},
		Blocks:   []Block{{Heading: "Тейлор", Paragraphs: []string{"тейлор"}}},
		Segments: []Segment{{Text: "тейлор"}},
		Notes: []NoteFragment{
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "section", N: 1}, Text: "Ряды\nтейлор"},
			{Note: "n1", Material: "m1", Locator: Locator{Kind: "page", N: 2}, Text: "Ряды\nтейлор"},
		},
	})
	hits := s.Hits()
	if len(hits) != 6 {
		t.Fatalf("hits = %+v", hits)
	}
	for _, h := range hits {
		want := ""
		if h.Section == SectionOwnNote && h.Locator.Kind == "section" {
			want = "Ряды"
		}
		if h.Heading != want {
			t.Fatalf("hit %s %+v: heading %q, want %q", h.Section, h.Locator, h.Heading, want)
		}
	}
}

// Without a heading line the heading is "", so the label reads «раздел N»; Passages and hits share one cut.
func TestHeadingOf(t *testing.T) {
	tests := []struct {
		text, want string
	}{
		{"", ""},
		{" \n ", ""},
		{headingZWSP + headingRLO, ""},
		{"\x01\x02", ""},
		{"Ряды", "Ряды"},
		{"  Ряды  \nтекст", "Ряды"},
		{strings.Repeat("ж", MaxHeadingRunes), strings.Repeat("ж", MaxHeadingRunes)},
		{strings.Repeat("ж", MaxHeadingRunes+1), strings.Repeat("ж", MaxHeadingRunes-1) + "…"},
	}
	for _, tt := range tests {
		if got := headingOf(tt.text); got != tt.want {
			t.Errorf("headingOf(%q) = %q, want %q", tt.text, got, tt.want)
		}
		if got := fragmentHeading(NoteFragment{Locator: Locator{Kind: "section", N: 1}, Text: tt.text}); got != tt.want {
			t.Errorf("fragmentHeading(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}

	got := Passages("тейлор", []LessonText{{Lesson: Lesson{Key: "note:n1"}, Document: Document{Notes: []NoteFragment{
		{Note: "n1", Material: "m1", Locator: Locator{Kind: "section", N: 1}, Text: strings.Repeat("ж", 200) + "\nтейлор"},
	}}}})
	if len(got) != 1 || got[0].Heading != strings.Repeat("ж", MaxHeadingRunes-1)+"…" {
		t.Fatalf("passages %+v", got)
	}
}
