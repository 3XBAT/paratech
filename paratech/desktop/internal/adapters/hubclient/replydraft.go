package hubclient

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxReplyDraftRunes bounds the streamed reply draft as the Hub does (ADR-0026 addendum): the rest is dropped.
const maxReplyDraftRunes = 4000

// replyDraft joins the deltas of a streamed step into display text: valid UTF-8 (a rune split across deltas waits for
// its rest, a broken byte is dropped), no control characters except line breaks and tabs, at most maxReplyDraftRunes
// characters. The text is untrusted and only ever shown; it is never parsed or stored.
type replyDraft struct {
	sb      strings.Builder
	runes   int
	pending []byte // the start of a rune the next delta completes; under utf8.UTFMax bytes
}

// add appends delta and reports whether the text changed.
func (d *replyDraft) add(delta string) bool {
	if delta == "" || d.runes >= maxReplyDraftRunes {
		return false
	}
	data := delta
	if len(d.pending) > 0 {
		data = string(d.pending) + delta
		d.pending = d.pending[:0]
	}
	changed := false
	for i := 0; i < len(data) && d.runes < maxReplyDraftRunes; {
		r, size := utf8.DecodeRuneInString(data[i:])
		if r == utf8.RuneError && size <= 1 {
			if !utf8.FullRuneInString(data[i:]) {
				d.pending = append(d.pending, data[i:]...)
				break
			}
			i++ // a broken byte
			continue
		}
		i += size
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		d.sb.WriteRune(r)
		d.runes++
		changed = true
	}
	return changed
}

// String is the text so far.
func (d *replyDraft) String() string { return d.sb.String() }
