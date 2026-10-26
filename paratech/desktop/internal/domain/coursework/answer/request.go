package answer

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Limits of the reader's input, those the Hub checks (hub domain llm/services/answer). Sizes are bytes of UTF-8,
// lengths characters.
const (
	// MaxQuestionRunes bounds the question, as the question argument of answer_question (tool catalog v2).
	MaxQuestionRunes = 500
	// MaxFragments bounds the fragments of one question.
	MaxFragments = 40
	// MaxFragmentBytes bounds the text of one fragment.
	MaxFragmentBytes = 8 << 10
	// MaxFragmentsBytes bounds the texts of all fragments together.
	MaxFragmentsBytes = 128 << 10

	// MaxOutputTokens is the output cap asked for: at most MaxClaims claims with their quotes.
	MaxOutputTokens = 4096

	// MaxScopeBytes bounds the reuse scope of a request.
	MaxScopeBytes = 64

	// keyPrefix starts every idempotency key of the reader; the version changes with the request shape. v2 adds the
	// reuse scope; keys of v1 (no scope) stay valid in stored answers.
	keyPrefix   = "ans:v2:"
	keyPrefixV1 = "ans:v1:"
	// chunkPrefix starts the chunk ID of a fragment: f1, f2, ... in the order sent.
	chunkPrefix = "f"
)

// Section is the part of a lesson a fragment comes from; the places are those of a knowledge base search hit.
type Section string

// Sections of a lesson a fragment may come from.
const (
	SectionSummary     Section = "summary"     // a section or a key point of the summary
	SectionLectureText Section = "lectureText" // a block of the lecture text
	SectionTranscript  Section = "transcript"  // consecutive segments of the transcript
)

// Valid reports whether s is one of the sections.
func (s Section) Valid() bool {
	return s == SectionSummary || s == SectionLectureText || s == SectionTranscript
}

// Source is the place in the student's lessons a fragment comes from, so a citation opens the lesson there. Index is
// as the knowledge base counts it: the summary text, the lecture text block or the first transcript segment. StartMs
// is where the block or the segments start in the recording, 0 for the summary. Title (one line, "" for an ad-hoc
// recording) and StartedAt name the lesson as it was when the answer was made.
type Source struct {
	Recording shared.RecordingID
	Section   Section
	Index     int
	StartMs   int64
	Title     string
	StartedAt time.Time
}

func (s Source) validate() error {
	switch {
	case s.Recording.Validate() != nil:
		return fmt.Errorf("%w: source recording", ErrInvalidState)
	case !s.Section.Valid():
		return fmt.Errorf("%w: source section", ErrInvalidState)
	case s.Index < 0 || s.StartMs < 0 || (s.Section == SectionSummary && s.StartMs != 0):
		return fmt.Errorf("%w: source place", ErrInvalidState)
	case !utf8.ValidString(s.Title) || strings.ContainsAny(s.Title, "\n\r\x00"):
		return fmt.Errorf("%w: source title", ErrInvalidState)
	}
	return nil
}

// Excerpt is a text of a lesson to answer from, with its place.
type Excerpt struct {
	Text   string
	Source Source
}

// Fragment is an excerpt as the reader gets it, addressed by its chunk ID.
type Fragment struct {
	ChunkID string
	Text    string
	Source  Source
}

// Request is a checked reader request: the question, the fragments, the untrusted data and the idempotency key.
// Build it with NewRequest.
type Request struct {
	question  string
	fragments []Fragment
	byChunk   map[string]int
	data      string
	key       string
}

// ParseQuestion trims s and checks it: 1..MaxQuestionRunes characters of valid UTF-8 on one line, without control or
// format characters. Error: ErrInvalidQuestion.
func ParseQuestion(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxQuestionRunes {
		return "", fmt.Errorf("%w: 1-%d characters", ErrInvalidQuestion, MaxQuestionRunes)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return "", fmt.Errorf("%w: a control or format character", ErrInvalidQuestion)
		}
	}
	return s, nil
}

// NewRequest builds the request for question (ParseQuestion) from 1..MaxFragments excerpts in the order given; the
// chunk IDs are f1, f2, ... Each text must be valid UTF-8 without NUL, not blank, at most MaxFragmentBytes, and all
// together at most MaxFragmentsBytes; each source must be a valid place. scope is the opaque reuse scope of the key
// (validScope): the same question about the same places is one paid answer only within one scope. Errors:
// ErrInvalidQuestion, ErrInvalidRequest.
func NewRequest(question string, excerpts []Excerpt, scope string) (Request, error) {
	q, err := ParseQuestion(question)
	if err != nil {
		return Request{}, err
	}
	if !validScope(scope) {
		return Request{}, fmt.Errorf("%w: scope must be 1-%d printable ASCII bytes without spaces", ErrInvalidRequest,
			MaxScopeBytes)
	}
	if len(excerpts) == 0 || len(excerpts) > MaxFragments {
		return Request{}, fmt.Errorf("%w: 1-%d fragments", ErrInvalidRequest, MaxFragments)
	}

	type wireFragment struct {
		ChunkID string `json:"chunk_id"`
		Text    string `json:"text"`
	}
	r := Request{question: q, byChunk: map[string]int{}}
	wire := make([]wireFragment, 0, len(excerpts))
	total := 0
	for i, e := range excerpts {
		if len(e.Text) > MaxFragmentBytes || !utf8.ValidString(e.Text) || strings.TrimSpace(e.Text) == "" ||
			strings.ContainsRune(e.Text, 0) {
			return Request{}, fmt.Errorf("%w: fragment %d must be 1-%d bytes of UTF-8, not blank, without NUL",
				ErrInvalidRequest, i+1, MaxFragmentBytes)
		}
		if err := e.Source.validate(); err != nil {
			return Request{}, fmt.Errorf("%w: fragment %d: %w", ErrInvalidRequest, i+1, err)
		}
		if total += len(e.Text); total > MaxFragmentsBytes {
			return Request{}, fmt.Errorf("%w: more than %d bytes of fragments", ErrInvalidRequest, MaxFragmentsBytes)
		}
		f := Fragment{ChunkID: chunkPrefix + strconv.Itoa(i+1), Text: e.Text, Source: e.Source}
		r.byChunk[f.ChunkID] = len(r.fragments)
		r.fragments = append(r.fragments, f)
		wire = append(wire, wireFragment{ChunkID: f.ChunkID, Text: f.Text})
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Strings and a slice of string pairs only: encoding cannot fail.
	_ = enc.Encode(struct {
		Question  string         `json:"question"`
		Fragments []wireFragment `json:"fragments"`
	}{q, wire})
	r.data = strings.TrimSuffix(b.String(), "\n")

	// The places are part of the key: the same text of another lesson is another answer. The scope comes last, after
	// a fixed number of parts per fragment, so it cannot pass for a place.
	sum := sha256.New()
	sum.Write([]byte(r.data))
	for _, f := range r.fragments {
		for _, part := range []string{string(f.Source.Recording), string(f.Source.Section), strconv.Itoa(f.Source.Index),
			strconv.FormatInt(f.Source.StartMs, 10)} {
			sum.Write([]byte{0})
			sum.Write([]byte(part))
		}
	}
	sum.Write([]byte{0})
	sum.Write([]byte(scope))
	r.key = fmt.Sprintf("%s%x", keyPrefix, sum.Sum(nil))
	return r, nil
}

// validScope reports whether s is 1..MaxScopeBytes of printable ASCII without spaces.
func validScope(s string) bool {
	if s == "" || len(s) > MaxScopeBytes {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// validKey reports whether k is a key of the reader, of any version.
func validKey(k string) bool {
	return (strings.HasPrefix(k, keyPrefix) || strings.HasPrefix(k, keyPrefixV1)) && len(k) == len(keyPrefix)+64
}

// Question returns the checked question.
func (r Request) Question() string { return r.question }

// Fragments returns a copy of the fragments, in the order sent.
func (r Request) Fragments() []Fragment { return append([]Fragment(nil), r.fragments...) }

// UntrustedData is the JSON object the reader reads as data: {"question","fragments":[{"chunk_id","text"}]}.
func (r Request) UntrustedData() string { return r.data }

// Key is the idempotency key: a digest of the data, the places and the scope, so the same question about the same
// fragments in the same scope is replayed by the Hub without a second charge and found again in the store, and any
// change is a new key.
func (r Request) Key() string { return r.key }

// IsZero reports whether r is the zero value.
func (r Request) IsZero() bool { return r.data == "" }

// fragment returns the fragment with chunk ID id.
func (r Request) fragment(id string) (Fragment, bool) {
	i, ok := r.byChunk[id]
	if !ok {
		return Fragment{}, false
	}
	return r.fragments[i], true
}
