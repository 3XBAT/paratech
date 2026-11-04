// Package chatsummary is the quarantined reader that summarizes the earlier requests of an agent chat for context
// compaction (OPERATION_TYPE_CHAT_SUMMARY, ADR-0028): the shape of its input and the strict check of its answer. The
// previous summary and the pairs are untrusted data (student texts and model replies); the answer is one plain text
// that the desktop only ever sends back as the summary turn of the history, never an instruction, an address or an
// action (hard rule 3).
//
// Wire shape (CompletionSpec.untrusted_data, no client instructions; CompletionResult.output):
//
//	input:  {"summary":"<previous summary or empty>","pairs":[{"student":"<text>","reply":"<text or empty>"}]}
//	answer: {"summary":"<text>"}
package chatsummary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/strictjson"
)

// Limits of the input. Sizes are bytes of UTF-8.
const (
	// MaxDataBytes bounds the whole untrusted data as sent.
	MaxDataBytes = 64 << 10
	// MaxPreviousBytes bounds the previous summary, as AgentTurn.history_summary.
	MaxPreviousBytes = 8 << 10
	// MinPairs and MaxPairs bound the requests summarized in one call: two window blocks at most (ADR-0028).
	MinPairs, MaxPairs = 1, 10
	// MaxStudentBytes bounds the student's text of one pair.
	MaxStudentBytes = 4 << 10
	// MaxReplyBytes bounds the reply of one pair.
	MaxReplyBytes = 2 << 10
)

// Limits of the answer: those of the desktop's HistorySummary and of AgentTurn.history_summary, so a summary the Hub
// returns is one the desktop can keep and send back.
const (
	MaxSummaryRunes = 4000
	MaxSummaryBytes = 8 << 10
)

// FailedOutput is the stored result of a reader answer that was charged but refused, so a retry with the same key
// repeats the refusal instead of paying again. It never passes ParseAnswer.
const FailedOutput = "{}"

// Keys of the input and the answer.
const (
	keySummary = "summary"
	keyPairs   = "pairs"
	keyStudent = "student"
	keyReply   = "reply"
)

// Errors of the reader (ARCH-18). None of them quotes the summary, a pair or the model's answer.
var (
	// ErrInvalidInput means the untrusted data of a request is not a valid reader input.
	ErrInvalidInput = errors.New("invalid chat_summary input")
	// ErrInvalidAnswer means the model's answer breaks the reader schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid chat_summary answer")
)

// Pair is one earlier request of the chat: the student's text and the reply it got (empty when there was none).
type Pair struct {
	Student string `json:"student"`
	Reply   string `json:"reply"`
}

// Input is a checked reader request. Build it with ParseInput.
type Input struct {
	previous string
	pairs    []Pair
}

// ParseInput checks the untrusted data of a request: at most MaxDataBytes, one JSON object with exactly the keys
// summary (at most MaxPreviousBytes, may be empty) and pairs (MinPairs..MaxPairs objects with exactly student, not
// blank and at most MaxStudentBytes, and reply, at most MaxReplyBytes and may be empty). No text may hold NUL.
func ParseInput(data string) (Input, error) {
	if len(data) > MaxDataBytes {
		return Input{}, fmt.Errorf("%w: the data is over %d bytes", ErrInvalidInput, MaxDataBytes)
	}
	var (
		in   Input
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(data, func(key string, d *json.Decoder) error {
		seen[key] = true
		switch key {
		case keySummary:
			var ok bool
			in.previous, ok = strictjson.ReadString(d)
			if !ok || len(in.previous) > MaxPreviousBytes || strings.ContainsRune(in.previous, 0) {
				return fmt.Errorf("%w: summary must be at most %d bytes without NUL", ErrInvalidInput, MaxPreviousBytes)
			}
		case keyPairs:
			in.pairs = []Pair{}
			return strictjson.ReadArray(d, func(d *json.Decoder) error {
				p, err := readPair(d)
				if err != nil {
					return err
				}
				if len(in.pairs) == MaxPairs {
					return fmt.Errorf("%w: at most %d pairs", ErrInvalidInput, MaxPairs)
				}
				in.pairs = append(in.pairs, p)
				return nil
			})
		default:
			return fmt.Errorf("%w: unknown key", ErrInvalidInput)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			return Input{}, err
		}
		return Input{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if !seen[keySummary] || !seen[keyPairs] {
		return Input{}, fmt.Errorf("%w: summary and pairs are required", ErrInvalidInput)
	}
	if len(in.pairs) < MinPairs {
		return Input{}, fmt.Errorf("%w: at least %d pair", ErrInvalidInput, MinPairs)
	}
	return in, nil
}

func readPair(d *json.Decoder) (Pair, error) {
	var p Pair
	seen := map[string]bool{}
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyStudent:
			p.Student, ok = strictjson.ReadString(d)
			ok = ok && len(p.Student) <= MaxStudentBytes && strings.TrimSpace(p.Student) != "" && !strings.ContainsRune(p.Student, 0)
		case keyReply:
			p.Reply, ok = strictjson.ReadString(d)
			ok = ok && len(p.Reply) <= MaxReplyBytes && !strings.ContainsRune(p.Reply, 0)
		}
		if !ok {
			return fmt.Errorf("%w: a pair is {student: 1-%d bytes, not blank, reply: 0-%d bytes}, without NUL", ErrInvalidInput,
				MaxStudentBytes, MaxReplyBytes)
		}
		return nil
	})
	if err != nil {
		return Pair{}, err
	}
	if !seen[keyStudent] || !seen[keyReply] {
		return Pair{}, fmt.Errorf("%w: a pair needs student and reply", ErrInvalidInput)
	}
	return p, nil
}

// ModelData is what the model reads: the checked input re-encoded as one JSON object, so every text is a JSON string
// value and cannot end the data or pose as another key. HTML characters are not escaped.
func (in Input) ModelData() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only a string and a slice of string pairs: encoding cannot fail.
	_ = enc.Encode(struct {
		Summary string `json:"summary"`
		Pairs   []Pair `json:"pairs"`
	}{in.previous, in.pairs})
	return strings.TrimSuffix(b.String(), "\n")
}

// Answer is a checked reader answer. Dropped counts the characters the check removed from the text.
type Answer struct {
	Summary string
	Dropped int
}

// ParseAnswer checks the model's output. The whole answer is refused unless it is exactly one JSON object with the one
// key summary, a string (whitespace and one Markdown code fence around the object are tolerated). The text is then
// cleaned, as it is plain text whose meaning these characters do not carry: a line break (CR LF, CR, U+2028, U+2029,
// NEL) becomes LF, any other whitespace (tab, no-break and other Unicode spaces) becomes a space, and every other
// character that is not printable is removed (control and format characters, bidi marks, embeddings, overrides and
// isolates, zero-width characters, U+FFFD). Surrounding whitespace is trimmed. The cleaned text must keep 1 to
// MaxSummaryRunes characters and at most MaxSummaryBytes; an empty, blank or longer one is refused, never cut, as a
// cut summary could change its meaning.
func (in Input) ParseAnswer(output string) (Answer, error) {
	body, ok := strictjson.Unfence(output)
	if !ok {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		raw  string
		seen bool
	)
	err := strictjson.DecodeObject(body, func(key string, d *json.Decoder) error {
		if key != keySummary {
			return fmt.Errorf("%w: an unknown key", ErrInvalidAnswer)
		}
		seen = true
		var ok bool
		if raw, ok = strictjson.ReadString(d); !ok {
			return fmt.Errorf("%w: summary is not a string", ErrInvalidAnswer)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAnswer) {
			return Answer{}, err
		}
		return Answer{}, fmt.Errorf("%w: %w", ErrInvalidAnswer, err)
	}
	if !seen {
		return Answer{}, fmt.Errorf("%w: summary is required", ErrInvalidAnswer)
	}

	text, dropped := clean(raw)
	switch {
	case text == "":
		return Answer{}, fmt.Errorf("%w: the summary is empty", ErrInvalidAnswer)
	case utf8.RuneCountInString(text) > MaxSummaryRunes || len(text) > MaxSummaryBytes:
		return Answer{}, fmt.Errorf("%w: the summary is over %d characters or %d bytes", ErrInvalidAnswer, MaxSummaryRunes, MaxSummaryBytes)
	}
	return Answer{Summary: text, Dropped: dropped}, nil
}

// Unicode line breaks other than CR and LF.
const (
	lineSeparator      = 0x2028
	paragraphSeparator = 0x2029
	nextLine           = 0x85
)

// clean maps line breaks to LF and other whitespace to a space, removes every other character that is not printable
// and trims the result; dropped counts the removed characters.
func clean(s string) (string, int) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var (
		b       strings.Builder
		dropped int
	)
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == lineSeparator || r == paragraphSeparator || r == nextLine:
			b.WriteByte('\n')
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.IsPrint(r) && r != utf8.RuneError && !bidi(r):
			b.WriteRune(r)
		default:
			dropped++
		}
	}
	return strings.TrimSpace(b.String()), dropped
}

// bidi reports the bidi marks, embeddings, overrides and isolates. unicode.IsPrint already refuses them (all Cf); they
// are listed so a later change of the printable rule keeps them out, as the desktop's check does.
func bidi(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F || r == 0x061C
}

// Encode is the canonical answer the Hub returns and stores.
func (a Answer) Encode() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only a string: encoding cannot fail.
	_ = enc.Encode(struct {
		Summary string `json:"summary"`
	}{a.Summary})
	return strings.TrimSuffix(b.String(), "\n")
}
