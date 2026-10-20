package knowledgebase

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Bounds of a search.
const (
	MinQueryRunes = 2
	MaxQueryRunes = 100
	// MaxHits bounds the hits of one search; the rest is reported as Truncated.
	MaxHits = 50
	// MaxSectionHits bounds the hits of one section of one lesson, so one long transcript does not fill the list.
	MaxSectionHits = 3
	// SnippetRunes bounds the text around a hit; at most snippetLead runes of it come before the match.
	SnippetRunes = 120
	snippetLead  = 40
	ellipsis     = "…"
)

// ErrInvalidQuery is returned by NewSearch for a query that is too short or too long.
var ErrInvalidQuery = errors.New("invalid knowledge base query")

// Section is where in a lesson a hit is.
type Section string

// Sections of a lesson, in the order a lesson is searched.
const (
	SectionTitle       Section = "title"
	SectionSummary     Section = "summary"
	SectionLectureText Section = "lectureText"
	SectionTranscript  Section = "transcript"
)

// Block is one block of a lecture text, as plain values.
type Block struct {
	Heading    string
	StartMs    int64
	Paragraphs []string
}

// Segment is one recognised segment of a transcript, as plain values.
type Segment struct {
	StartMs int64
	Text    string
}

// Document is everything searched of one lesson. Every text is untrusted.
type Document struct {
	Recording shared.RecordingID
	Title     string
	// Summary is the summary text followed by its key points; nil: no summary.
	Summary  []string
	Blocks   []Block
	Segments []Segment
}

// Snippet is the text around a hit, one plain line cut to SnippetRunes: Match is the matched text as written, Before
// and After surround it ("…" where the text was cut).
type Snippet struct {
	Before string
	Match  string
	After  string
}

// Hit is one place a query was found. Index is the block (SectionLectureText) or segment (SectionTranscript) index,
// for the summary 0 for its text and i+1 for its key point i, 0 for the title. StartMs is where the block or segment
// starts in the recording, 0 for the title and the summary.
type Hit struct {
	Recording shared.RecordingID
	Section   Section
	Index     int
	StartMs   int64
	Snippet   Snippet
}

// Search collects the hits of one query over the lessons scanned, in scan order: per lesson the title, the summary,
// the lecture text and the transcript, each text matched once, at most MaxSectionHits per section of a lesson and
// MaxHits in all. Build it with NewSearch.
type Search struct {
	query     []rune // folded
	hits      []Hit
	truncated bool
}

// NewSearch prepares a search for query: spaces are collapsed, it is matched case-insensitively and "ё" as "е". It
// must have MinQueryRunes to MaxQueryRunes characters, else ErrInvalidQuery.
func NewSearch(query string) (*Search, error) {
	q := strings.Join(strings.Fields(strings.Map(blank, query)), " ")
	n := utf8.RuneCountInString(q)
	if n < MinQueryRunes || n > MaxQueryRunes {
		return nil, fmt.Errorf("%w: %d characters, want %d-%d", ErrInvalidQuery, n, MinQueryRunes, MaxQueryRunes)
	}
	return &Search{query: []rune(strings.Map(fold, q))}, nil
}

// Scan adds the hits of one lesson; once Full, it adds nothing.
func (s *Search) Scan(d Document) {
	if s.Full() {
		return
	}
	s.section(d.Recording, SectionTitle, func(add func(int, int64, string) bool) {
		add(0, 0, d.Title)
	})
	s.section(d.Recording, SectionSummary, func(add func(int, int64, string) bool) {
		for i, text := range d.Summary {
			if !add(i, 0, text) {
				return
			}
		}
	})
	s.section(d.Recording, SectionLectureText, func(add func(int, int64, string) bool) {
		for i, b := range d.Blocks {
			if !add(i, b.StartMs, b.Heading) {
				return
			}
			for _, p := range b.Paragraphs {
				if !add(i, b.StartMs, p) {
					return
				}
			}
		}
	})
	s.section(d.Recording, SectionTranscript, func(add func(int, int64, string) bool) {
		for i, seg := range d.Segments {
			if !add(i, seg.StartMs, seg.Text) {
				return
			}
		}
	})
}

// section runs one section of a lesson: add matches a text and reports whether more texts are wanted. A block whose
// heading matched is not matched again in its paragraphs.
func (s *Search) section(rec shared.RecordingID, sec Section, texts func(add func(index int, startMs int64, text string) bool)) {
	found, last := 0, -1
	texts(func(index int, startMs int64, text string) bool {
		if index == last {
			return true
		}
		snip, ok := s.match(text)
		if !ok {
			return true
		}
		if found == MaxSectionHits || s.Full() {
			s.truncated = true
			return false
		}
		s.hits = append(s.hits, Hit{Recording: rec, Section: sec, Index: index, StartMs: startMs, Snippet: snip})
		found, last = found+1, index
		return true
	})
}

// match finds the first match of the query in text and cuts the snippet around it.
func (s *Search) match(text string) (Snippet, bool) {
	start, ok := s.find(text)
	if !ok {
		return Snippet{}, false
	}
	// Invalid UTF-8 is read the same way by find and by the conversion to runes: one U+FFFD per bad byte.
	runes := []rune(text)
	end := start + len(s.query)

	from := max(start-snippetLead, 0)
	to := min(end+max(SnippetRunes-(end-from), 0), len(runes))
	snip := Snippet{
		Before: strings.TrimLeftFunc(plain(runes[from:start]), unicode.IsSpace),
		Match:  plain(runes[start:end]),
		After:  strings.TrimRightFunc(plain(runes[end:to]), unicode.IsSpace),
	}
	if from > 0 {
		snip.Before = ellipsis + snip.Before
	}
	if to < len(runes) {
		snip.After += ellipsis
	}
	return snip, true
}

// find returns the rune position of the first match in text. It allocates nothing: most texts do not match, and a
// search reads every text of the base.
func (s *Search) find(text string) (int, bool) {
	for i, n := 0, 0; i < len(text); n++ {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if fold(r) == s.query[0] && s.matchesAt(text[i:]) {
			return n, true
		}
	}
	return 0, false
}

// matchesAt reports whether rest starts with the query after its first rune.
func (s *Search) matchesAt(rest string) bool {
	for _, want := range s.query[1:] {
		if rest == "" {
			return false
		}
		r, size := utf8.DecodeRuneInString(rest)
		if fold(r) != want {
			return false
		}
		rest = rest[size:]
	}
	return true
}

// Full reports that MaxHits are found: scanning more lessons adds nothing.
func (s *Search) Full() bool { return len(s.hits) >= MaxHits }

// Hits returns the hits found, in scan order.
func (s *Search) Hits() []Hit { return append([]Hit(nil), s.hits...) }

// Truncated reports that the hits may be incomplete: MaxSectionHits left some out, or MaxHits are found.
func (s *Search) Truncated() bool { return s.truncated || s.Full() }

// plain makes untrusted text one plain line: format characters (bidi overrides, zero-width ones) removed, control
// characters and line breaks as spaces, runs of spaces collapsed; spaces at the ends are kept as one.
func plain(rs []rune) string {
	var b strings.Builder
	space := false
	for _, r := range rs {
		switch {
		case unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsSpace(r) || unicode.IsControl(r) || r == 0x2028 || r == 0x2029:
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// blank turns the characters a query cannot match into spaces before they are collapsed.
func blank(r rune) rune {
	switch {
	case unicode.Is(unicode.Cf, r):
		return -1
	case unicode.IsControl(r) || r == 0x2028 || r == 0x2029:
		return ' '
	}
	return r
}
