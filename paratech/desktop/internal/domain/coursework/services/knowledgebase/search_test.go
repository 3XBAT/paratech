package knowledgebase

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewSearch(t *testing.T) {
	tests := []struct {
		query string
		ok    bool
	}{
		{"ab", true},
		{"  a  ", false},
		{"a", false},
		{"", false},
		{"\u200b\u200bя", false}, // format characters do not count
		{strings.Repeat("я", MaxQueryRunes), true},
		{strings.Repeat("я", MaxQueryRunes+1), false},
		{"a \n b", true},
	}
	for _, tc := range tests {
		_, err := NewSearch(tc.query)
		if tc.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalidQuery)) {
			t.Errorf("NewSearch(%q) = %v, want ok %v", tc.query, err, tc.ok)
		}
	}
}

func mustSearch(t *testing.T, q string) *Search {
	t.Helper()
	s, err := NewSearch(q)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSearchMatches(t *testing.T) {
	tests := []struct {
		name  string
		query string
		text  string
		want  Snippet
		found bool
	}{
		{name: "case-insensitive", query: "КВАНТ", text: "Теория кванта", want: Snippet{Before: "Теория ", Match: "квант", After: "а"}, found: true},
		{name: "ё as е", query: "елка", text: "Ёлка зелёная", want: Snippet{Match: "Ёлка", After: " зелёная"}, found: true},
		{name: "spaces collapse in the query", query: "теория   поля", text: "теория поля", want: Snippet{Match: "теория поля"}, found: true},
		{name: "line break matches a space", query: "теория поля", text: "теория\nполя", want: Snippet{Match: "теория поля"}, found: true},
		{name: "no match", query: "химия", text: "физика"},
		{name: "control and bidi characters are cut out", query: "ab", text: "x\u202e\u0007 ab", want: Snippet{Before: "x ", Match: "ab"}, found: true},
		{name: "invalid UTF-8 keeps positions", query: "ab", text: "\xff\xfe ab cd", want: Snippet{Before: "�� ", Match: "ab", After: " cd"}, found: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustSearch(t, tc.query)
			s.Scan(Document{Recording: "r1", Title: tc.text})
			hits := s.Hits()
			if !tc.found {
				if len(hits) != 0 {
					t.Fatalf("hits = %+v, want none", hits)
				}
				return
			}
			if len(hits) != 1 || hits[0].Snippet != tc.want || hits[0].Section != SectionTitle || hits[0].Recording != "r1" {
				t.Fatalf("hits = %+v, want one title hit %+v", hits, tc.want)
			}
		})
	}
}

func TestSearchSnippetIsCut(t *testing.T) {
	before, after := strings.Repeat("а", 100), strings.Repeat("б", 300)
	s := mustSearch(t, "ключ")
	s.Scan(Document{Recording: "r1", Summary: []string{before + "ключ" + after}})
	hits := s.Hits()
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	snip := hits[0].Snippet
	if !strings.HasPrefix(snip.Before, ellipsis) || !strings.HasSuffix(snip.After, ellipsis) || snip.Match != "ключ" {
		t.Fatalf("snippet = %+v", snip)
	}
	n := utf8.RuneCountInString(strings.TrimPrefix(snip.Before, ellipsis) + snip.Match + strings.TrimSuffix(snip.After, ellipsis))
	if n != SnippetRunes || utf8.RuneCountInString(snip.Before) != snippetLead+1 {
		t.Fatalf("snippet has %d runes (before %d), want %d", n, utf8.RuneCountInString(snip.Before), SnippetRunes)
	}
}

func TestSearchSections(t *testing.T) {
	d := Document{
		Recording: "r1",
		Title:     "Физика: механика",
		Summary:   []string{"Речь о механике.", "Законы Ньютона", "Механика жидкостей"},
		Blocks: []Block{
			{Heading: "Введение", StartMs: 0, Paragraphs: []string{"О чём курс."}},
			{Heading: "Механика", StartMs: 60_000, Paragraphs: []string{"Механика изучает движение."}},
			{Heading: "Итоги", StartMs: 120_000, Paragraphs: []string{"Кинематика", "и механика"}},
		},
		Segments: []Segment{{StartMs: 1000, Text: " механика"}, {StartMs: 2000, Text: " другое"}, {StartMs: 3000, Text: " Механика!"}},
	}
	s := mustSearch(t, "механик")
	s.Scan(d)

	type place struct {
		sec   Section
		index int
		start int64
	}
	var got []place
	for _, h := range s.Hits() {
		got = append(got, place{h.Section, h.Index, h.StartMs})
	}
	want := []place{
		{SectionTitle, 0, 0},
		{SectionSummary, 0, 0}, {SectionSummary, 2, 0},
		{SectionLectureText, 1, 60_000}, {SectionLectureText, 2, 120_000}, // one hit per block
		{SectionTranscript, 0, 1000}, {SectionTranscript, 2, 3000},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("hits = %v\nwant %v", got, want)
	}
	if s.Truncated() {
		t.Error("truncated without a cap")
	}
}

func TestSearchCaps(t *testing.T) {
	segs := make([]Segment, 10)
	for i := range segs {
		segs[i] = Segment{StartMs: int64(i), Text: "слово"}
	}
	s := mustSearch(t, "слово")
	s.Scan(Document{Recording: "r1", Segments: segs})
	if n := len(s.Hits()); n != MaxSectionHits || !s.Truncated() {
		t.Fatalf("one section: %d hits, truncated %v; want %d, true", n, s.Truncated(), MaxSectionHits)
	}

	s = mustSearch(t, "слово")
	for i := 0; !s.Full(); i++ {
		s.Scan(Document{Recording: "r", Title: "слово"})
		if i > MaxHits {
			t.Fatal("never full")
		}
	}
	s.Scan(Document{Recording: "late", Title: "слово"})
	if n := len(s.Hits()); n != MaxHits || !s.Truncated() {
		t.Fatalf("all: %d hits, truncated %v; want %d, true", n, s.Truncated(), MaxHits)
	}
}
