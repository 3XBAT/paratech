package anthropic

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/values"
)

// maxDraftKeyBytes bounds the top-level key the reader remembers; a longer key is never the text argument.
const maxDraftKeyBytes = 16

// Roles of the JSON string being read.
const (
	roleNone = iota
	roleKey
	roleText
	roleOther
)

// replyText reads the top-level text argument of a reply call from the partial_json chunks of its tool_use block
// (docs/refs/anthropic.md section 8) and returns the decoded text of each chunk as soon as it is whole: escapes,
// \uXXXX with surrogate pairs and UTF-8 sequences may be split across chunks. It never fails: JSON it cannot follow
// ends the draft, and the use case checks the whole answer anyway. Its state is a few bytes whatever the input size.
// Strings of other keys and of nested values are skipped, never returned.
type replyText struct {
	depth     int
	expectKey bool
	textNext  bool // the last top-level key was the text argument
	role      int
	escaped   bool   // a backslash of a skipped string
	key       []byte // bytes of the current top-level key, at most maxDraftKeyBytes
	keyLong   bool
	esc       []byte // escape sequence of the text after its backslash, at most "uXXXX"
	inEscape  bool
	high      rune   // high surrogate waiting for its low half, 0 when none
	tail      []byte // incomplete UTF-8 sequence at the end of the last chunk
	done      bool   // the text ended, the object closed or the JSON broke: nothing more is returned
}

// Feed reads the next chunk and returns the text it completes, cleaned for display (draftText).
func (r *replyText) Feed(chunk string) string {
	if r.done {
		return ""
	}
	var out strings.Builder
	out.Write(r.tail)
	r.tail = r.tail[:0]
	for i := 0; i < len(chunk) && !r.done; i++ {
		c := chunk[i]
		switch r.role {
		case roleText:
			r.text(&out, c)
		case roleKey, roleOther:
			r.skip(c)
		default:
			r.structure(c)
		}
	}
	s := out.String()
	if !r.done {
		var tail string
		s, tail = cutIncompleteUTF8(s)
		r.tail = append(r.tail, tail...)
	}
	return draftText(s)
}

// structure reads a byte outside any string.
func (r *replyText) structure(c byte) {
	switch c {
	case '{':
		r.depth++
		if r.depth == 1 {
			r.expectKey = true
		}
	case '[':
		r.depth++
	case '}', ']':
		r.depth--
		if r.depth <= 0 {
			r.done = true
		}
	case ',':
		if r.depth == 1 {
			r.expectKey, r.textNext = true, false
		}
	case ':':
		if r.depth == 1 {
			r.expectKey = false
		}
	case '"':
		switch {
		case r.depth == 1 && r.expectKey:
			r.role, r.key, r.keyLong = roleKey, r.key[:0], false
		case r.depth == 1 && r.textNext:
			r.role = roleText
		default:
			r.role = roleOther
		}
	}
}

// skip reads a byte of a key or of a string that is not the text, keeping a short key.
func (r *replyText) skip(c byte) {
	switch {
	case r.escaped:
		r.escaped = false
		r.keyLong = r.keyLong || r.role == roleKey // an escaped key is never taken for the text argument
	case c == '\\':
		r.escaped = true
	case c == '"':
		if r.role == roleKey {
			r.textNext = !r.keyLong && string(r.key) == values.ReplyTextArgument
		}
		r.role = roleNone
	case r.role == roleKey:
		if len(r.key) < maxDraftKeyBytes {
			r.key = append(r.key, c)
		} else {
			r.keyLong = true
		}
	}
}

// text reads a byte of the text argument's string.
func (r *replyText) text(out *strings.Builder, c byte) {
	if r.inEscape {
		r.escape(out, c)
		return
	}
	switch c {
	case '\\':
		r.inEscape, r.esc = true, r.esc[:0]
	case '"':
		r.lone(out)
		r.done = true
	default:
		r.lone(out)
		out.WriteByte(c)
	}
}

// escape reads a byte of an escape sequence.
func (r *replyText) escape(out *strings.Builder, c byte) {
	if len(r.esc) == 0 {
		if c == 'u' {
			r.esc = append(r.esc, c)
			return
		}
		r.inEscape = false
		r.lone(out)
		if d, ok := simpleEscapes[c]; ok {
			out.WriteByte(d)
		} else {
			r.done = true // not JSON
		}
		return
	}
	if hexValue(c) < 0 {
		r.inEscape, r.done = false, true
		return
	}
	r.esc = append(r.esc, c)
	if len(r.esc) < 5 {
		return
	}
	r.inEscape = false
	var v rune
	for _, h := range r.esc[1:] {
		v = v<<4 | hexValue(h)
	}
	switch {
	case utf16.IsSurrogate(v) && v < 0xDC00: // high half
		r.lone(out)
		r.high = v
	case utf16.IsSurrogate(v): // low half
		if r.high != 0 {
			out.WriteRune(utf16.DecodeRune(r.high, v))
			r.high = 0
		} else {
			out.WriteRune(utf8.RuneError)
		}
	default:
		r.lone(out)
		out.WriteRune(v)
	}
}

// lone writes a high surrogate that got no low half as U+FFFD.
func (r *replyText) lone(out *strings.Builder) {
	if r.high != 0 {
		out.WriteRune(utf8.RuneError)
		r.high = 0
	}
}

var simpleEscapes = map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

func hexValue(c byte) rune {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0')
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10
	}
	return -1
}

// cutIncompleteUTF8 splits s before a trailing UTF-8 sequence that is not complete yet (the rule of the Connect
// handler's cutIncompleteRune); invalid bytes stay in head.
func cutIncompleteUTF8(s string) (head, tail string) {
	for i := len(s) - 1; i >= 0 && i > len(s)-utf8.UTFMax; i-- {
		if utf8.RuneStart(s[i]) {
			if !utf8.FullRuneInString(s[i:]) {
				return s[:i], s[i:]
			}
			break
		}
	}
	return s, ""
}

// draftText makes s valid UTF-8 and drops control characters other than line breaks and tabs: the draft is shown as
// plain text.
func draftText(s string) string {
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
