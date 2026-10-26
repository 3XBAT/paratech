package answer

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Bounds of the draft scanner beyond the answer limits.
const (
	// maxDraftDepth stops at nesting no answer has, so garbage cannot grow the stack.
	maxDraftDepth = 32
	// maxKeyRunes is enough to tell the keys of the schema; a longer key never equals one of them.
	maxKeyRunes = 16
)

// draftFrame is an open object or array of the partial answer.
type draftFrame struct {
	obj      bool
	key      string // the last key of an object
	keyNext  bool   // the object expects a key
	textDone bool   // the text of this claim was taken
}

// Draft returns the claim texts written so far in a partial reader output (ADR-0026): the text of each object of the
// claims array of the first JSON object, in order, the last one possibly cut off. JSON escapes are decoded, invalid
// UTF-8 and control characters but line breaks and tabs are dropped, blank texts skipped; at most MaxClaims texts of at
// most MaxClaimRunes runes. It never fails: anything it cannot read ends the draft. The draft is untrusted model text
// for display only; the answer is Parse of the whole output.
func Draft(partial string) []string {
	start := strings.IndexByte(partial, '{')
	if start < 0 {
		return nil
	}
	in := partial[start:]

	var (
		texts []string
		stack []draftFrame
	)
	for i := 0; i < len(in) && len(texts) < MaxClaims; {
		c := in[i]
		switch c {
		case '{', '[':
			if len(stack) == maxDraftDepth {
				return texts
			}
			stack = append(stack, draftFrame{obj: c == '{', keyNext: c == '{'})
			i++
			continue
		case '}', ']':
			if len(stack) <= 1 {
				return texts // the answer object is closed
			}
			stack = stack[:len(stack)-1]
			i++
			continue
		}

		top := &stack[len(stack)-1]
		switch c {
		case ':':
			if top.obj {
				top.keyNext = false
			}
			i++
		case ',':
			if top.obj {
				top.keyNext, top.key = true, ""
			}
			i++
		case '"':
			isKey := top.obj && top.keyNext
			isText := !isKey && claimText(stack)
			limit := 0
			switch {
			case isKey:
				limit = maxKeyRunes
			case isText:
				limit = MaxClaimRunes
			}
			s, n, closed := draftString(in[i+1:], limit)
			i += 1 + n
			switch {
			case isKey:
				top.key, top.keyNext = s, false
			case isText:
				top.textDone = true
				if strings.TrimSpace(s) != "" {
					texts = append(texts, s)
				}
			}
			if !closed {
				return texts
			}
		default:
			i++ // numbers, literals, white space and anything else
		}
	}
	return texts
}

// claimText reports whether the next string value is the text of a claim: root object → claims → array → object →
// text, the first text of that object.
func claimText(stack []draftFrame) bool {
	if len(stack) != 3 {
		return false
	}
	root, list, claim := stack[0], stack[1], stack[2]
	return root.obj && root.key == keyClaims && !list.obj && claim.obj && claim.key == keyText && !claim.keyNext &&
		!claim.textDone
}

// draftString reads a JSON string body (after the opening quote) and returns up to limit runes of it decoded, the bytes
// consumed (with the closing quote) and whether the string was closed. An escape cut off at the end is dropped.
func draftString(s string, limit int) (string, int, bool) {
	var (
		b     strings.Builder
		runes int
	)
	add := func(r rune) {
		if runes >= limit || r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return
		}
		b.WriteRune(r)
		runes++
	}
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '"':
			return b.String(), i + 1, true
		case '\\':
			if i+1 >= len(s) {
				return b.String(), len(s), false
			}
			switch e := s[i+1]; e {
			case 'u':
				r, w := unicodeEscape(s[i:])
				if w == 0 {
					return b.String(), len(s), false
				}
				add(r)
				i += w
			case '"', '\\', '/':
				add(rune(e))
				i += 2
			case 'n':
				add('\n')
				i += 2
			case 't':
				add('\t')
				i += 2
			default:
				i += 2 // \b, \f, \r are control characters; anything else is not JSON
			}
		default:
			r, w := utf8.DecodeRuneInString(s[i:])
			add(r)
			i += w
		}
	}
	return b.String(), len(s), false
}

// unicodeEscape decodes \uXXXX at the start of s, joining a surrogate pair. w is the bytes used, 0 when s ends inside
// the escape or its pair; a bad or lone escape gives utf8.RuneError.
func unicodeEscape(s string) (rune, int) {
	if len(s) < 6 {
		return 0, 0
	}
	r1, ok := hex4(s[2:6])
	if !ok {
		return utf8.RuneError, 2
	}
	if !utf16.IsSurrogate(r1) {
		return r1, 6
	}
	rest := s[6:]
	if len(rest) < 6 && (rest == "" || rest == `\` || strings.HasPrefix(rest, `\u`)) {
		return 0, 0
	}
	if strings.HasPrefix(rest, `\u`) {
		if r2, ok := hex4(rest[2:6]); ok {
			if r := utf16.DecodeRune(r1, r2); r != utf8.RuneError {
				return r, 12
			}
		}
	}
	return utf8.RuneError, 6
}

func hex4(s string) (rune, bool) {
	var r rune
	for i := range len(s) {
		c := s[i]
		var d byte
		switch {
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= c && c <= 'f':
			d = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		r = r<<4 | rune(d)
	}
	return r, true
}
