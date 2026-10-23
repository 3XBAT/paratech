// Package answer is the quarantined reader that answers a student's question from lesson and material fragments
// (OPERATION_TYPE_ANSWER, ADR-0022): the shape of its input and the strict check of its answer. The question and the
// fragments are untrusted data; the answer is claims, each citing fragments by chunk ID with a quote, and the parts of
// the question the fragments do not answer. Whether a quote really is in its fragment is the desktop's check
// (coursework/services/citation), which matches quotes loosely; the Hub checks the shape, the bounds and that every
// cited chunk ID is one of the fragments sent.
//
// Wire shape (CompletionSpec.untrusted_data, no client instructions; CompletionResult.output):
//
//	input:  {"question":"<text>","fragments":[{"chunk_id":"<id>","text":"<text>"}]}
//	answer: {"claims":[{"text":"<text>","cites":[{"chunk_id":"<id of a fragment>","quote":"<text>"}]}],
//	         "missing":["<text>"]}
package answer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/services/strictjson"
	"paratech/hub/internal/domain/llm/values"
)

// Limits of the input. Sizes are bytes of UTF-8, lengths are characters (runes).
const (
	// MaxQuestionRunes bounds the question, as the question argument of answer_question in the tool catalog.
	MaxQuestionRunes = 500
	// MaxFragments bounds the fragments of one question.
	MaxFragments = 40
	// MaxFragmentBytes bounds the text of one fragment.
	MaxFragmentBytes = 8 << 10
	// MaxFragmentsBytes bounds the texts of all fragments together, as much as INSPECT_MATERIAL reads.
	MaxFragmentsBytes = 128 << 10
)

// Limits of the answer.
const (
	// MaxClaims bounds the claims of one answer.
	MaxClaims = 8
	// MaxClaimRunes bounds the text of one claim.
	MaxClaimRunes = 400
	// MaxCites bounds the citations of one claim; a claim has at least one.
	MaxCites = 3
	// MaxQuoteRunes bounds one quote.
	MaxQuoteRunes = 200
	// MaxMissing bounds the unanswered parts; MaxMissingRunes bounds each.
	MaxMissing, MaxMissingRunes = 5, 300
)

// FailedOutput is the stored result of a reader answer that was charged but refused, so a retry with the same key
// repeats the refusal instead of paying again. It never passes ParseAnswer.
const FailedOutput = "{}"

// Keys of the input and the answer.
const (
	keyQuestion  = "question"
	keyFragments = "fragments"
	keyChunkID   = "chunk_id"
	keyText      = "text"

	keyClaims  = "claims"
	keyMissing = "missing"
	keyCites   = "cites"
	keyQuote   = "quote"
)

// Errors of the reader (ARCH-18). None of them quotes the question, a fragment or the model's answer.
var (
	// ErrInvalidInput means the untrusted data of a request is not a valid reader input.
	ErrInvalidInput = errors.New("invalid answer operation input")
	// ErrInvalidAnswer means the model's answer breaks the reader schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid answer operation output")
)

// Fragment is one excerpt of a lesson or a material the model may answer from, addressed by the desktop's chunk ID.
type Fragment struct {
	ChunkID string `json:"chunk_id"`
	Text    string `json:"text"`
}

// Input is a checked reader request. Build it with ParseInput.
type Input struct {
	question  string
	fragments []Fragment
	ids       map[string]bool
}

// ParseInput checks the untrusted data of a request: one JSON object with exactly the keys question (non-blank, at
// most MaxQuestionRunes characters without control characters but line breaks and tabs) and fragments (1-MaxFragments
// with distinct valid chunk IDs and non-blank texts of at most MaxFragmentBytes each, MaxFragmentsBytes together,
// without NUL).
func ParseInput(data string) (Input, error) {
	var (
		in    = Input{ids: map[string]bool{}}
		seen  = map[string]bool{}
		total int
	)
	err := strictjson.DecodeObject(data, func(key string, d *json.Decoder) error {
		seen[key] = true
		switch key {
		case keyQuestion:
			var ok bool
			in.question, ok = strictjson.ReadString(d)
			if !ok || !boundedText(in.question, MaxQuestionRunes, true) {
				return fmt.Errorf("%w: question must be 1-%d characters, not blank, without control characters", ErrInvalidInput, MaxQuestionRunes)
			}
		case keyFragments:
			in.fragments = []Fragment{}
			return strictjson.ReadArray(d, func(d *json.Decoder) error {
				f, err := readFragment(d)
				if err != nil {
					return err
				}
				total += len(f.Text)
				if len(in.fragments) == MaxFragments || in.ids[f.ChunkID] || total > MaxFragmentsBytes {
					return fmt.Errorf("%w: at most %d fragments with distinct chunk ids and %d bytes of text", ErrInvalidInput,
						MaxFragments, MaxFragmentsBytes)
				}
				in.ids[f.ChunkID] = true
				in.fragments = append(in.fragments, f)
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
	if !seen[keyQuestion] || !seen[keyFragments] {
		return Input{}, fmt.Errorf("%w: question and fragments are required", ErrInvalidInput)
	}
	if len(in.fragments) == 0 {
		return Input{}, fmt.Errorf("%w: at least one fragment", ErrInvalidInput)
	}
	return in, nil
}

func readFragment(d *json.Decoder) (Fragment, error) {
	var f Fragment
	seen := map[string]bool{}
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyChunkID:
			f.ChunkID, ok = strictjson.ReadString(d)
			ok = ok && values.ValidID(f.ChunkID)
		case keyText:
			f.Text, ok = strictjson.ReadString(d)
			ok = ok && len(f.Text) <= MaxFragmentBytes && strings.TrimSpace(f.Text) != "" && !strings.ContainsRune(f.Text, 0)
		}
		if !ok {
			return fmt.Errorf("%w: a fragment is {chunk_id: an id, text: 1-%d bytes, not blank, without NUL}", ErrInvalidInput, MaxFragmentBytes)
		}
		return nil
	})
	if err != nil {
		return Fragment{}, err
	}
	if !seen[keyChunkID] || !seen[keyText] {
		return Fragment{}, fmt.Errorf("%w: a fragment needs chunk_id and text", ErrInvalidInput)
	}
	return f, nil
}

// ModelData is what the model reads: the checked input re-encoded as one JSON object, so every text is a JSON string
// value and cannot end the data or pose as another key. HTML characters are not escaped.
func (in Input) ModelData() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings and a slice of string pairs: encoding cannot fail.
	_ = enc.Encode(struct {
		Question  string     `json:"question"`
		Fragments []Fragment `json:"fragments"`
	}{in.question, in.fragments})
	return strings.TrimSuffix(b.String(), "\n")
}

// Cite is one citation: the chunk ID of a fragment sent and the quote the claim rests on.
type Cite struct {
	ChunkID string `json:"chunk_id"`
	Quote   string `json:"quote"`
}

// Claim is one statement of the answer with its citations.
type Claim struct {
	Text  string `json:"text"`
	Cites []Cite `json:"cites"`
}

// Answer is a checked reader answer.
type Answer struct {
	Claims  []Claim
	Missing []string
}

// ParseAnswer checks the model's output against the schema: exactly the keys claims and missing once each, at most
// MaxClaims claims (each exactly text and cites, 1-MaxCites citations of exactly chunk_id, one of the fragments sent,
// and quote), at most MaxMissing unanswered parts, every text bounded and without control characters but line breaks
// and tabs, and at least one claim or one unanswered part. Whitespace and one Markdown code fence around the object are
// tolerated; anything else (another key, free text, a second object) refuses the whole answer.
func (in Input) ParseAnswer(output string) (Answer, error) {
	body, ok := strictjson.Unfence(output)
	if !ok {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		a    Answer
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(body, func(key string, d *json.Decoder) error {
		seen[key] = true
		ok := false
		switch key {
		case keyClaims:
			a.Claims = []Claim{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				c, err := in.readClaim(d)
				if err != nil || len(a.Claims) == MaxClaims {
					return strictjson.ErrShape
				}
				a.Claims = append(a.Claims, c)
				return nil
			}) == nil
		case keyMissing:
			a.Missing = []string{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				s, read := strictjson.ReadString(d)
				if !read || len(a.Missing) == MaxMissing || !boundedText(s, MaxMissingRunes, true) {
					return strictjson.ErrShape
				}
				a.Missing = append(a.Missing, s)
				return nil
			}) == nil
		}
		if !ok {
			return fmt.Errorf("%w: %s is not a value of the schema", ErrInvalidAnswer, safeKey(key))
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAnswer) {
			return Answer{}, err
		}
		return Answer{}, fmt.Errorf("%w: %w", ErrInvalidAnswer, err)
	}
	if !seen[keyClaims] || !seen[keyMissing] {
		return Answer{}, fmt.Errorf("%w: claims and missing are required", ErrInvalidAnswer)
	}
	if len(a.Claims) == 0 && len(a.Missing) == 0 {
		return Answer{}, fmt.Errorf("%w: an answer has a claim or a missing part", ErrInvalidAnswer)
	}
	return a, nil
}

func (in Input) readClaim(d *json.Decoder) (Claim, error) {
	var c Claim
	seen := map[string]bool{}
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyText:
			c.Text, ok = strictjson.ReadString(d)
			ok = ok && boundedText(c.Text, MaxClaimRunes, true)
		case keyCites:
			c.Cites = []Cite{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				cite, err := in.readCite(d)
				if err != nil || len(c.Cites) == MaxCites {
					return strictjson.ErrShape
				}
				c.Cites = append(c.Cites, cite)
				return nil
			}) == nil
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || len(seen) != 2 || !seen[keyText] || !seen[keyCites] || len(c.Cites) == 0 {
		return Claim{}, strictjson.ErrShape
	}
	return c, nil
}

func (in Input) readCite(d *json.Decoder) (Cite, error) {
	var c Cite
	seen := map[string]bool{}
	err := strictjson.ReadObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyChunkID:
			c.ChunkID, ok = strictjson.ReadString(d)
			// Only a fragment that was sent: a model cannot point the desktop at another lesson.
			ok = ok && in.ids[c.ChunkID]
		case keyQuote:
			c.Quote, ok = strictjson.ReadString(d)
			ok = ok && boundedText(c.Quote, MaxQuoteRunes, true)
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || len(seen) != 2 || !seen[keyChunkID] || !seen[keyQuote] {
		return Cite{}, strictjson.ErrShape
	}
	return c, nil
}

// Encode is the canonical answer the Hub returns and stores: the keys in the order of the wire shape, empty lists as
// [].
func (a Answer) Encode() string {
	claims := make([]Claim, 0, len(a.Claims))
	for _, c := range a.Claims {
		if c.Cites == nil {
			c.Cites = []Cite{}
		}
		claims = append(claims, c)
	}
	missing := a.Missing
	if missing == nil {
		missing = []string{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings and slices of structs of strings: encoding cannot fail.
	_ = enc.Encode(struct {
		Claims  []Claim  `json:"claims"`
		Missing []string `json:"missing"`
	}{claims, missing})
	return strings.TrimSuffix(b.String(), "\n")
}

// boundedText is valid UTF-8 of 1..maxRunes characters, not blank, without control characters (a multiline text may
// hold line breaks and tabs).
func boundedText(s string, maxRunes int, multiline bool) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	blank := true
	for _, r := range s {
		lineBreak := multiline && (r == '\n' || r == '\t')
		if unicode.IsControl(r) && !lineBreak {
			return false
		}
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	return !blank
}

// safeKey names a schema key in an error; any other key (model text) is not quoted.
func safeKey(key string) string {
	if key == keyClaims || key == keyMissing {
		return key
	}
	return "an unknown key"
}
