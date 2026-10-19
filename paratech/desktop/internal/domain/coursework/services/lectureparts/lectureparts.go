package lectureparts

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/lecturetext"
)

// Errors of the planner (ARCH-18). Messages name positions, never text.
var (
	// ErrEmpty is returned by Plan for a transcript without speech.
	ErrEmpty = errors.New("lecture text: the transcript is empty")

	// ErrInvalidSegment is returned by Plan for a segment that is not one line of valid UTF-8 text.
	ErrInvalidSegment = errors.New("lecture text: invalid transcript segment")
)

// Sizing of the parts. The desktop leaves max_output_tokens to the model alias of the Hub (0), whose limit it does not
// know, so a conservative 4096 output tokens is assumed. The formatted text is about as long as its input and
// Cyrillic takes about 2.5-3 characters (5-6 bytes of UTF-8) per token, so a part of MaxPartBytes is about 1200-1500
// tokens plus the JSON around it: well inside the limit, with room for a model that writes more than it reads.
// MaxPartMs keeps a part short in slow speech. A 3-hour lesson (250-300 KB of text) gives about 40 parts; beyond
// MaxParts both limits grow by the same factor until the parts fit.
const (
	MaxPartBytes = 7 << 10
	MaxPartMs    = 6 * 60 * 1000
	MaxParts     = lecturetext.MaxParts

	// The context of a part: the segments of the previous part that start in its last ContextMs, at most
	// MaxContextBytes (the end of the text is kept).
	ContextMs       = 25 * 1000
	MaxContextBytes = 2 << 10
)

// Segment is one recognised segment of the transcript, in the order of the transcript.
type Segment struct {
	StartMs int64
	Text    string
}

// Item is a segment of a part: its index in the transcript, its start and its trimmed text.
type Item struct {
	Index   int
	StartMs int64
	Text    string
}

// Part is one part of the lecture text: the segments to format and the context before them.
type Part struct {
	context string
	items   []Item
}

// Context returns the end of the previous part, for continuity only; empty for the first part.
func (p Part) Context() string { return p.context }

// Items returns a copy of the segments of the part, in order; never empty.
func (p Part) Items() []Item { return append([]Item(nil), p.items...) }

// StartMs returns the start of the first segment of the part.
func (p Part) StartMs() int64 { return p.items[0].StartMs }

// Starts maps the segment indexes the model named for the blocks of the part (from) to their start times. An index
// outside the part starts at the start of the part; an index of a blank segment inside the part at the segment before
// it; a start before the start of the block before is moved to it. So the starts never leave the part and never go
// back in time, whatever the model wrote.
func (p Part) Starts(froms []int) []int64 {
	out := make([]int64, len(froms))
	prev := p.StartMs()
	for i, from := range froms {
		start := p.StartMs()
		if from >= p.items[0].Index && from <= p.items[len(p.items)-1].Index {
			for _, it := range p.items {
				if it.Index > from {
					break
				}
				start = it.StartMs
			}
		}
		prev = max(prev, start)
		out[i] = prev
	}
	return out
}

// Plan splits the segments into parts (see the package comment). Blank segments are skipped, but the indexes stay
// those of the transcript. Starts that go back in time are moved forward to the start before them. Errors:
// ErrEmpty without speech, ErrInvalidSegment for a segment that is not one line of valid UTF-8.
func Plan(segments []Segment) ([]Part, error) {
	var items []Item
	var last int64
	for i, s := range segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		if !utf8.ValidString(text) || strings.ContainsFunc(text, notInLine) {
			return nil, fmt.Errorf("%w: segment %d", ErrInvalidSegment, i)
		}
		last = max(last, s.StartMs)
		items = append(items, Item{Index: i, StartMs: last, Text: text})
	}
	if len(items) == 0 {
		return nil, ErrEmpty
	}

	for scale := 1; ; scale++ {
		groups := split(items, MaxPartBytes*scale, int64(MaxPartMs*scale))
		if len(groups) > MaxParts {
			continue
		}
		parts := make([]Part, len(groups))
		for i, g := range groups {
			parts[i] = Part{items: g}
			if i > 0 {
				parts[i].context = contextOf(groups[i-1])
			}
		}
		return parts, nil
	}
}

// split cuts items greedily: a part ends before the item that would take it over maxBytes or that starts maxMs or
// more after the part.
func split(items []Item, maxBytes int, maxMs int64) [][]Item {
	var groups [][]Item
	var cur []Item
	size := 0
	for _, it := range items {
		n := len(it.Text) + 1
		if len(cur) > 0 && (size+n > maxBytes || it.StartMs-cur[0].StartMs >= maxMs) {
			groups, cur, size = append(groups, cur), nil, 0
		}
		cur, size = append(cur, it), size+n
	}
	return append(groups, cur)
}

// contextOf is the text of the items of a part that start in its last ContextMs, at most MaxContextBytes from the end.
func contextOf(items []Item) string {
	end := items[len(items)-1].StartMs
	first := len(items) - 1
	for first > 0 && end-items[first-1].StartMs <= ContextMs {
		first--
	}
	texts := make([]string, 0, len(items)-first)
	for _, it := range items[first:] {
		texts = append(texts, it.Text)
	}
	text := strings.Join(texts, " ")
	if len(text) <= MaxContextBytes {
		return text
	}
	cut := len(text) - MaxContextBytes
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return text[cut:]
}

// Merge joins the blocks of the parts, in order, into the lecture text. The first paragraph of a part that repeats
// the last paragraph before it (the same words, ignoring case and spacing) is dropped as a repetition of the context,
// and so is a first block left without paragraphs. A first block whose heading repeats the heading of the block
// before continues it: its paragraphs are added to that block while it has room (lecturetext.MaxParagraphs).
func Merge(parts [][]lecturetext.Block) ([]lecturetext.Block, error) {
	var out []lecturetext.Block
	for _, blocks := range parts {
		for i, b := range blocks {
			if i > 0 || len(out) == 0 {
				out = append(out, b)
				continue
			}
			prev := out[len(out)-1]
			paragraphs := b.Paragraphs()
			prevParagraphs := prev.Paragraphs()
			if normalize(paragraphs[0]) == normalize(prevParagraphs[len(prevParagraphs)-1]) {
				paragraphs = paragraphs[1:]
			}
			if len(paragraphs) == 0 {
				continue
			}

			heading, start := b.Heading(), b.StartMs()
			if normalize(heading) == normalize(prev.Heading()) && len(prevParagraphs)+len(paragraphs) <= lecturetext.MaxParagraphs {
				heading, start, paragraphs = prev.Heading(), prev.StartMs(), append(prevParagraphs, paragraphs...)
				out = out[:len(out)-1]
			}
			nb, err := lecturetext.NewBlock(heading, start, paragraphs)
			if err != nil {
				return nil, err
			}
			out = append(out, nb)
		}
	}
	return out, nil
}

func normalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// notInLine rejects control characters and the line and paragraph separators.
func notInLine(r rune) bool { return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 }
