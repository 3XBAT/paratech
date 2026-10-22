package material

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
)

// Limits of an extracted text.
const (
	// MaxTextBytes bounds the text kept of one file; the rest is cut (Text.Truncated).
	MaxTextBytes = 4 << 20

	// MaxFragments bounds the pages, slides or fragments of one text.
	MaxFragments = 5000

	// FragmentRunes is the size a text without pages or slides is cut into fragments of, at line ends.
	FragmentRunes = 3000
)

// LocatorKind tells what a locator counts: pages of a document, slides of a presentation, or fragments of a text
// without either.
type LocatorKind string

// Locator kinds.
const (
	LocatorPage     LocatorKind = "page"
	LocatorSlide    LocatorKind = "slide"
	LocatorFragment LocatorKind = "fragment"
)

func (k LocatorKind) valid() bool {
	return k == LocatorPage || k == LocatorSlide || k == LocatorFragment
}

// label is the heading of a fragment in the text sent to a model and shown to the student.
func (k LocatorKind) label() string {
	switch k {
	case LocatorPage:
		return "Страница"
	case LocatorSlide:
		return "Слайд"
	}
	return "Фрагмент"
}

// Locator is where a piece of text is in its file: page, slide or fragment N (from 1).
type Locator struct {
	Kind LocatorKind
	N    int
}

// String is the heading of the fragment, for example «[Слайд 3]».
func (l Locator) String() string { return "[" + l.Kind.label() + " " + strconv.Itoa(l.N) + "]" }

// Fragment is one page, slide or fragment: its locator and its lines, each one plain line.
type Fragment struct {
	Locator Locator
	Lines   []string
}

// Text is the text extracted from a file, in reading order. It is a validated value: build it with NewText.
type Text struct {
	fragments []Fragment
	truncated bool
}

// NewText cleans the lines (values.CleanText; empty ones are dropped), drops fragments left without lines and checks
// the locators: one kind, numbers from 1 rising. truncated tells that the extraction stopped at a limit. A text
// above MaxTextBytes or MaxFragments is cut and marked truncated. A text without lines is valid and Empty.
func NewText(fragments []Fragment, truncated bool) (Text, error) {
	t := Text{truncated: truncated}
	size, last, full := 0, 0, false
	var kind LocatorKind
	for _, f := range fragments {
		if !f.Locator.Kind.valid() || f.Locator.N <= last || (kind != "" && f.Locator.Kind != kind) {
			return Text{}, fmt.Errorf("%w: locator %s %d", ErrInvalidText, f.Locator.Kind, f.Locator.N)
		}
		kind, last = f.Locator.Kind, f.Locator.N
		if full {
			continue // the locators are still checked
		}
		var lines []string
		for _, l := range f.Lines {
			l = values.CleanText(l)
			if l == "" {
				continue
			}
			need := len(l) + 1
			if len(lines) == 0 {
				need += len(f.Locator.String()) + 1 // the heading is part of String
			}
			if size+need > MaxTextBytes {
				full = true
				break
			}
			size += need
			lines = append(lines, l)
		}
		if len(lines) == 0 {
			continue
		}
		if len(t.fragments) == MaxFragments {
			full = true
			continue
		}
		t.fragments = append(t.fragments, Fragment{Locator: f.Locator, Lines: lines})
	}
	t.truncated = t.truncated || full
	return t, nil
}

// FragmentsOf cuts lines of a text without pages or slides into fragments of about FragmentRunes characters, at line
// ends, numbered from 1.
func FragmentsOf(lines []string) []Fragment {
	var (
		out   []Fragment
		cur   []string
		runes int
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Fragment{Locator: Locator{Kind: LocatorFragment, N: len(out) + 1}, Lines: cur})
			cur, runes = nil, 0
		}
	}
	for _, l := range lines {
		n := utf8.RuneCountInString(l)
		if runes > 0 && runes+n > FragmentRunes {
			flush()
		}
		cur = append(cur, l)
		runes += n
	}
	flush()
	return out
}

// Fragments returns a copy of the fragments.
func (t Text) Fragments() []Fragment {
	out := make([]Fragment, len(t.fragments))
	for i, f := range t.fragments {
		out[i] = Fragment{Locator: f.Locator, Lines: append([]string(nil), f.Lines...)}
	}
	return out
}

// Empty reports whether the text has no line.
func (t Text) Empty() bool { return len(t.fragments) == 0 }

// Truncated reports whether the text was cut at a limit, so it is not the whole file.
func (t Text) Truncated() bool { return t.truncated }

// Lines returns the text as lines: each fragment is its heading (Locator.String) and its lines.
func (t Text) Lines() []string {
	var out []string
	for _, f := range t.fragments {
		out = append(out, f.Locator.String())
		out = append(out, f.Lines...)
	}
	return out
}

// String is Lines joined by line breaks.
func (t Text) String() string { return strings.Join(t.Lines(), "\n") }

// Head returns String cut to at most maxBytes at a line end, or inside the first line at a character boundary.
func (t Text) Head(maxBytes int) string {
	s := t.String()
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return cut[:i]
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// Digest is the sha256 of String with the truncation mark: the version of the text an inspection was made for.
func (t Text) Digest() Digest {
	h := sha256.New()
	h.Write([]byte(t.String()))
	if t.truncated {
		h.Write([]byte{0, 't'})
	}
	return Digest(hex.EncodeToString(h.Sum(nil)))
}
