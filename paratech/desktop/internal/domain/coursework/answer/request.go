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
	SectionOwnNote     Section = "ownNote"     // a fragment of the student's own note (ADR-0027)
)

// Valid reports whether s is one of the sections.
func (s Section) Valid() bool {
	return s == SectionSummary || s == SectionLectureText || s == SectionTranscript || s == SectionOwnNote
}

// LocatorKind is the kind of place of a fragment in a file of a note.
type LocatorKind string

// Kinds of place in a file of a note, as the library reads its text.
const (
	LocatorPage     LocatorKind = "page"
	LocatorSlide    LocatorKind = "slide"
	LocatorSection  LocatorKind = "section"
	LocatorFragment LocatorKind = "fragment"
)

// Locator is where a fragment of a note is in its file: the kind and N from 1.
type Locator struct {
	Kind LocatorKind
	N    int
}

func (l Locator) valid() bool {
	switch l.Kind {
	case LocatorPage, LocatorSlide, LocatorSection, LocatorFragment:
		return l.N >= 1
	}
	return false
}

// MaxHeadingRunes bounds the heading of a section of a note a source keeps for its label.
const MaxHeadingRunes = 120

// Source is the place in the student's lessons a fragment comes from, so a citation opens the lesson there. Index is
// as the knowledge base counts it: the summary text, the lecture text block, the first transcript segment or the
// fragment of the lesson's notes. StartMs is where the block or the segments start in the recording, 0 for the
// summary and a note. Title (one line, "" for an ad-hoc recording) and StartedAt name the lesson as it was when the
// answer was made.
//
// A source of SectionOwnNote has no Recording (a note-only lecture has none, and the note is what opens) but its
// Note, Material and Locator; Heading is the heading line of a section of a note, "" otherwise. The other sections
// have a Recording and none of these.
type Source struct {
	Recording shared.RecordingID
	Section   Section
	Index     int
	StartMs   int64
	Title     string
	StartedAt time.Time
	Note      shared.NoteID
	Material  shared.MaterialID
	Locator   Locator
	Heading   string
}

func (s Source) validate() error {
	if !s.Section.Valid() {
		return fmt.Errorf("%w: source section", ErrInvalidState)
	}
	if s.Section == SectionOwnNote {
		switch {
		case s.Recording != "":
			return fmt.Errorf("%w: source recording of a note", ErrInvalidState)
		case s.Note.Validate() != nil || s.Material.Validate() != nil:
			return fmt.Errorf("%w: source note", ErrInvalidState)
		case !s.Locator.valid() || (s.Heading != "" && s.Locator.Kind != LocatorSection):
			return fmt.Errorf("%w: source locator", ErrInvalidState)
		case !oneLine(s.Heading) || utf8.RuneCountInString(s.Heading) > MaxHeadingRunes:
			return fmt.Errorf("%w: source heading", ErrInvalidState)
		}
	} else {
		switch {
		case s.Recording.Validate() != nil:
			return fmt.Errorf("%w: source recording", ErrInvalidState)
		case s.Note != "" || s.Material != "" || s.Locator != (Locator{}) || s.Heading != "":
			return fmt.Errorf("%w: source note of a recording", ErrInvalidState)
		}
	}
	switch {
	case s.Index < 0 || s.StartMs < 0 || ((s.Section == SectionSummary || s.Section == SectionOwnNote) && s.StartMs != 0):
		return fmt.Errorf("%w: source place", ErrInvalidState)
	case !oneLine(s.Title):
		return fmt.Errorf("%w: source title", ErrInvalidState)
	}
	return nil
}

// oneLine reports whether s is valid UTF-8 without line breaks or NUL.
func oneLine(s string) bool { return utf8.ValidString(s) && !strings.ContainsAny(s, "\n\r\x00") }

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
	// a number of parts per fragment fixed by its section, so it cannot pass for a place. A fragment of a recording
	// keeps the parts it had before notes, so the keys of stored answers are found again.
	sum := sha256.New()
	sum.Write([]byte(r.data))
	for _, f := range r.fragments {
		parts := []string{string(f.Source.Recording), string(f.Source.Section), strconv.Itoa(f.Source.Index),
			strconv.FormatInt(f.Source.StartMs, 10)}
		if f.Source.Section == SectionOwnNote {
			parts = append(parts, string(f.Source.Note), string(f.Source.Material), string(f.Source.Locator.Kind),
				strconv.Itoa(f.Source.Locator.N))
		}
		for _, part := range parts {
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
