package lecturetext

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits of a block. The heading and paragraph limits are those of the lecture_text answer
// (domain/coursework/services/prompts), so every valid answer fits; a block may gain paragraphs when the merge joins
// a block that continues the previous heading, up to MaxParagraphs.
const (
	MaxHeadingRunes   = 200
	MaxParagraphRunes = 4000
	MaxParagraphs     = 200
)

// Block is one logical block of the lecture text: a short heading, the time in the recording where it starts and its
// paragraphs. It is a validated value: build it with NewBlock.
type Block struct {
	heading    string
	startMs    int64
	paragraphs []string
}

// NewBlock validates and trims a block: a one-line heading of 1 to MaxHeadingRunes characters, a start of at least 0
// ms and 1 to MaxParagraphs paragraphs of 1 to MaxParagraphRunes characters each (line breaks and tabs allowed).
func NewBlock(heading string, startMs int64, paragraphs []string) (Block, error) {
	heading = strings.TrimSpace(heading)
	if heading == "" || !utf8.ValidString(heading) || utf8.RuneCountInString(heading) > MaxHeadingRunes ||
		strings.ContainsFunc(heading, notInLine) {
		return Block{}, fmt.Errorf("%w: heading must be one line of 1-%d characters", ErrInvalidBlock, MaxHeadingRunes)
	}
	if startMs < 0 {
		return Block{}, fmt.Errorf("%w: negative start", ErrInvalidBlock)
	}
	if len(paragraphs) == 0 || len(paragraphs) > MaxParagraphs {
		return Block{}, fmt.Errorf("%w: 1-%d paragraphs", ErrInvalidBlock, MaxParagraphs)
	}
	out := make([]string, 0, len(paragraphs))
	for i, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" || !utf8.ValidString(p) || utf8.RuneCountInString(p) > MaxParagraphRunes || strings.ContainsFunc(p, badControl) {
			return Block{}, fmt.Errorf("%w: paragraph %d must be 1-%d characters without control characters",
				ErrInvalidBlock, i, MaxParagraphRunes)
		}
		out = append(out, p)
	}
	return Block{heading: heading, startMs: startMs, paragraphs: out}, nil
}

// Heading returns the heading of the block.
func (b Block) Heading() string { return b.heading }

// StartMs returns where the block starts in the recording, in milliseconds.
func (b Block) StartMs() int64 { return b.startMs }

// Paragraphs returns a copy of the paragraphs.
func (b Block) Paragraphs() []string { return slices.Clone(b.paragraphs) }

// Equal reports whether both blocks hold the same heading, start and paragraphs.
func (b Block) Equal(o Block) bool {
	return b.heading == o.heading && b.startMs == o.startMs && slices.Equal(b.paragraphs, o.paragraphs)
}

// size is the bytes of text of the block.
func (b Block) size() int {
	n := len(b.heading)
	for _, p := range b.paragraphs {
		n += len(p)
	}
	return n
}

// BlockState is the stored form of a block.
type BlockState struct {
	Heading    string
	StartMs    int64
	Paragraphs []string
}

// State returns the stored form of the block.
func (b Block) State() BlockState {
	return BlockState{Heading: b.heading, StartMs: b.startMs, Paragraphs: b.Paragraphs()}
}

func badControl(r rune) bool {
	return (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') || bidiControl(r)
}

// notInLine also rejects the line and paragraph separators, which break a line in many renderers.
func notInLine(r rune) bool {
	return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || bidiControl(r)
}

// bidiControl reports the directional controls and tag characters, which can make displayed text differ from what it
// is; the answer parser rejects them too.
func bidiControl(r rune) bool {
	switch {
	case r == 0x200E, r == 0x200F, r == 0x061C:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return false
}
