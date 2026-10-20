package transcript

import (
	"strings"
	"unicode"
)

// Non-speech marks (TASK-110). Where nobody speaks whisper writes marks instead of words: "[BLANK_AUDIO]",
// "[ Silence ]", "[MUSIC]", "(музыка)", "*music*", "♪♪". They are model output that whisper.cpp does not filter; its
// own examples strip them (examples/python/whisper_processor.py removes "[BLANK_AUDIO]", examples/talk-llama drops every
// [...] and (...) group), src/whisper.cpp prints special tokens as [_..._] and lists the music symbols among its
// non-speech tokens. The rule is conservative, so speech is never lost: a segment holds no speech only when nothing but
// such marks, punctuation and spaces is left; inside speech only the square-bracket marks are removed, as parentheses
// may hold speech such as "(x + 1)".

// markPunct is the punctuation a mark may contain besides letters and spaces.
const markPunct = ".,!?:;'’\"«»-…"

// minMarkLetters keeps one-letter groups such as "a[i]" or "(x)" as speech.
const minMarkLetters = 2

// Speech returns the segment as speech, without whisper's square-bracket marks, and false when it holds no speech at
// all. A text without marks is returned unchanged.
func (s Segment) Speech() (Segment, bool) {
	if !hasWords(s.Text) {
		return Segment{}, false
	}
	if text, changed := stripMarks(s.Text); changed {
		s.Text = text
	}
	return s, true
}

// SpeechSegments returns the segments that hold speech (Segment.Speech), in order.
func SpeechSegments(segs []Segment) []Segment {
	out := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if sp, ok := s.Speech(); ok {
			out = append(out, sp)
		}
	}
	return out
}

// Speech returns the segments of the document that hold speech (SpeechSegments). Readers of the text use it, so the
// marks reach neither the screen nor the model.
func (d Document) Speech() []Segment { return SpeechSegments(d.segments) }

// HasSpeech reports whether any segment of the document holds speech.
func (d Document) HasSpeech() bool {
	for _, s := range d.segments {
		if hasWords(s.Text) {
			return true
		}
	}
	return false
}

// hasWords reports whether text holds anything but marks, punctuation, music symbols and spaces.
func hasWords(text string) bool {
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if j := markEnd(rs, i, true); j > i {
			i = j
			continue
		}
		if r := rs[i]; !unicode.IsSpace(r) && !unicode.IsPunct(r) && !music(r) {
			return true
		}
	}
	return false
}

// stripMarks removes the square-bracket marks from text; changed is false when there are none.
func stripMarks(text string) (out string, changed bool) {
	rs := []rune(text)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] == '[' {
			if j := markEnd(rs, i, false); j > i {
				i, changed = j, true
				b.WriteByte(' ')
				continue
			}
		}
		b.WriteRune(rs[i])
	}
	if !changed {
		return text, false
	}
	return strings.Join(strings.Fields(b.String()), " "), true
}

// markEnd returns the index of the closing rune of the mark that opens at rs[i], or -1. Square brackets always
// count; parentheses and stars only for whole segments, since inside speech they may hold words.
func markEnd(rs []rune, i int, whole bool) int {
	var closer rune
	switch rs[i] {
	case '[':
		closer = ']'
	case '(':
		closer = ')'
	case '*':
		closer = '*'
	default:
		return -1
	}
	if closer != ']' && !whole {
		return -1
	}
	for j := i + 1; j < len(rs); j++ {
		if rs[j] != closer {
			continue
		}
		content := rs[i+1 : j]
		if (closer == ']' && special(content)) || descriptive(content, closer == ']') {
			return j
		}
		return -1
	}
	return -1
}

// special reports a whisper special token such as [_BEG_] or [_TT_150].
func special(c []rune) bool {
	if len(c) < 2 || c[0] != '_' {
		return false
	}
	for _, r := range c {
		if r != '_' && (r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// descriptive reports the content of a mark that describes a sound: at least minMarkLetters letters, otherwise only
// spaces, markPunct and, in square brackets, underscores ("BLANK_AUDIO"). Digits and math symbols make it speech.
func descriptive(c []rune, underscore bool) bool {
	letters := 0
	for _, r := range c {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsSpace(r), strings.ContainsRune(markPunct, r), underscore && r == '_':
		default:
			return false
		}
	}
	return letters >= minMarkLetters
}

// music reports the music symbols whisper writes for music (♩ ♪ ♫ ♬ ♭ ♮ ♯, and the emoji 🎵 🎶).
func music(r rune) bool {
	return (r >= '♩' && r <= '♯') || r == '\U0001F3B5' || r == '\U0001F3B6'
}
