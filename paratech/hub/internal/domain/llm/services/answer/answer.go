// Package answer is the quarantined reader that answers a student's question from lesson and material fragments
// (OPERATION_TYPE_ANSWER, ADR-0022): the shape of its input and the strict check of its answer. The question and the
// fragments are untrusted data; the answer is claims, each citing fragments by chunk ID with a quote, and the parts of
// the question the fragments do not answer. The answer has no count or length limits of its own (owner, TASK-366): only
// the output cap of the model bounds it. The Hub checks the shape, that every cited chunk ID is one of the fragments
// sent and that its quote occurs in that fragment, read as loosely as the desktop's final check
// (coursework/services/citation); a claim, citation or missing part that fails is dropped, not the whole answer.
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
	// ErrInvalidAnswer means the model's answer is not one object of the reader schema or keeps nothing valid; the
	// whole answer is refused.
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
			if !ok || utf8.RuneCountInString(in.question) > MaxQuestionRunes || !plainText(in.question) {
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

// Answer is a checked reader answer. Dropped counts the claims, citations and missing parts left out by the check.
type Answer struct {
	Claims  []Claim
	Missing []string
	Dropped int
}

// ParseAnswer checks the model's output against the schema. The whole answer is refused unless it is exactly one JSON
// object with the keys claims and missing once each, both arrays of syntactically valid JSON (whitespace and one
// Markdown code fence around it are tolerated). Inside them each part is checked on its own and dropped when it fails:
// a citation that is not exactly chunk_id (one of the fragments sent) and quote (plain text that occurs in that
// fragment), a claim that is not exactly text (plain text) and cites or keeps no citation, a missing part that is not
// plain text. Plain text is valid UTF-8, not blank, without control characters but line breaks and tabs. An answer
// that keeps no claim and no missing part is refused.
//
// The cost stays linear in the output: every part is read once, and a quote is searched only in its own fragment
// (at most MaxFragmentBytes), normalized once per call.
func (in Input) ParseAnswer(output string) (Answer, error) {
	body, ok := strictjson.Unfence(output)
	if !ok {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		a     Answer
		seen  = map[string]bool{}
		texts = in.searchTexts()
	)
	err := strictjson.DecodeObject(body, func(key string, d *json.Decoder) error {
		seen[key] = true
		ok := false
		switch key {
		case keyClaims:
			a.Claims = []Claim{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				raw, _, read := strictjson.ReadRaw(d)
				if !read {
					return strictjson.ErrShape
				}
				c, dropped, valid := in.checkClaim(raw, texts)
				a.Dropped += dropped
				if valid {
					a.Claims = append(a.Claims, c)
				}
				return nil
			}) == nil
		case keyMissing:
			a.Missing = []string{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				raw, _, read := strictjson.ReadRaw(d)
				if !read {
					return strictjson.ErrShape
				}
				if s, valid := plainString(raw); valid {
					a.Missing = append(a.Missing, s)
				} else {
					a.Dropped++
				}
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

// checkClaim reads one element of claims: the claim with its valid citations, how many parts were dropped (the
// citations that failed, and the claim itself when it fails or keeps no citation), and whether the claim is kept.
func (in Input) checkClaim(raw string, texts map[string]string) (Claim, int, bool) {
	var (
		c       Claim
		dropped int
		seen    = map[string]bool{}
	)
	err := strictjson.DecodeObject(raw, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyText:
			c.Text, ok = strictjson.ReadString(d)
			ok = ok && plainText(c.Text)
		case keyCites:
			c.Cites = []Cite{}
			ok = strictjson.ReadArray(d, func(d *json.Decoder) error {
				raw, _, read := strictjson.ReadRaw(d)
				if !read {
					return strictjson.ErrShape
				}
				if cite, valid := in.checkCite(raw, texts); valid {
					c.Cites = append(c.Cites, cite)
				} else {
					dropped++
				}
				return nil
			}) == nil
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || !seen[keyText] || !seen[keyCites] || len(c.Cites) == 0 {
		return Claim{}, dropped + 1, false
	}
	return c, dropped, true
}

// checkCite reads one element of cites: exactly chunk_id, one of the fragments sent, and quote, plain text that occurs
// in that fragment.
func (in Input) checkCite(raw string, texts map[string]string) (Cite, bool) {
	var (
		c    Cite
		seen = map[string]bool{}
	)
	err := strictjson.DecodeObject(raw, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyChunkID:
			c.ChunkID, ok = strictjson.ReadString(d)
			// Only a fragment that was sent: a model cannot point the desktop at another lesson.
			ok = ok && in.ids[c.ChunkID]
		case keyQuote:
			c.Quote, ok = strictjson.ReadString(d)
			ok = ok && plainText(c.Quote)
		}
		if !ok {
			return strictjson.ErrShape
		}
		return nil
	})
	if err != nil || !seen[keyChunkID] || !seen[keyQuote] {
		return Cite{}, false
	}
	return c, quoteIn(c.Quote, texts[c.ChunkID])
}

// searchTexts is the text of every fragment by chunk ID, normalized for the quote search.
func (in Input) searchTexts() map[string]string {
	texts := make(map[string]string, len(in.fragments))
	for _, f := range in.fragments {
		texts[f.ChunkID] = normalize(f.Text)
	}
	return texts
}

// plainString reads raw as one JSON string of plain text.
func plainString(raw string) (string, bool) {
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return "", false
	}
	return s, plainText(s)
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

// plainText is valid UTF-8, not blank, without control characters but line breaks and tabs.
func plainText(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	blank := true
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
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
