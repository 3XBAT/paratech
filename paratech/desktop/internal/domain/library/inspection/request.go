package inspection

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

// Limits of the reader's input, those the Hub checks (hub domain llm/services/inspect).
const (
	// MaxTextBytes bounds the material text; the beginning of a longer text is sent.
	MaxTextBytes = 128 << 10

	// MaxCandidates bounds the candidate subjects.
	MaxCandidates = 100

	// MaxNameRunes bounds a candidate name (ADR-0022).
	MaxNameRunes = 80

	// MaxOutputTokens is the output cap asked for: the answer is one short JSON object.
	MaxOutputTokens = 256

	// keyPrefix starts every idempotency key of the reader; the version changes with the request shape.
	keyPrefix = "insp:v1:"
)

// Candidate is a subject the reader may name: its ID and its name, data for the model.
type Candidate struct {
	ID   shared.SubjectID
	Name string
}

// CandidateName fits a subject name to the reader's rule (ADR-0022: letters, digits, spaces and ".,-()", at most
// MaxNameRunes): other characters become spaces, runs of spaces collapse, the ends are trimmed and a longer name is
// cut. "" means nothing is left and the candidate is dropped. A library name may hold ":" or be 200 characters long.
func CandidateName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(".,-()", r) {
			return r
		}
		return ' '
	}, strings.ToValidUTF8(s, " "))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxNameRunes {
		s = strings.TrimSpace(string([]rune(s)[:MaxNameRunes]))
	}
	return s
}

// Request is a checked reader request: its untrusted data and its idempotency key. Build it with NewRequest.
type Request struct {
	data       string
	key        string
	candidates map[shared.SubjectID]bool
}

// NewRequest builds the request for the text of material m on day today. The candidates' names are fitted with
// CandidateName; a candidate with an invalid ID, a repeated ID or no name left is dropped, and at most MaxCandidates
// are kept, in order. text must be valid UTF-8 without NUL, not blank and at most MaxTextBytes. attempt (from 1)
// is part of the key, so a new attempt after a refusal is a new request for the Hub. Error: ErrInvalidRequest.
func NewRequest(m shared.MaterialID, today time.Time, cands []Candidate, text string, attempt int) (Request, error) {
	switch {
	case m.Validate() != nil:
		return Request{}, fmt.Errorf("%w: material id", ErrInvalidRequest)
	case attempt < 1 || attempt > MaxAttempts:
		return Request{}, fmt.Errorf("%w: attempt %d", ErrInvalidRequest, attempt)
	case len(text) > MaxTextBytes || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0):
		return Request{}, fmt.Errorf("%w: text must be 1-%d bytes of UTF-8, not blank, without NUL", ErrInvalidRequest, MaxTextBytes)
	case today.Year() < 1 || today.Year() > 9999:
		return Request{}, fmt.Errorf("%w: today", ErrInvalidRequest)
	}

	type subject struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	r := Request{candidates: map[shared.SubjectID]bool{}}
	subjects := []subject{}
	for _, c := range cands {
		name := CandidateName(c.Name)
		if len(subjects) == MaxCandidates || name == "" || c.ID.Validate() != nil || r.candidates[c.ID] {
			continue
		}
		r.candidates[c.ID] = true
		subjects = append(subjects, subject{ID: string(c.ID), Name: name})
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Strings and a slice of string pairs only: encoding cannot fail.
	_ = enc.Encode(struct {
		Today        string    `json:"today"`
		Subjects     []subject `json:"subjects"`
		MaterialText string    `json:"material_text"`
	}{today.Format(time.DateOnly), subjects, text})
	r.data = strings.TrimSuffix(b.String(), "\n")

	sum := sha256.New()
	for _, part := range []string{string(m), r.data, strconv.Itoa(attempt)} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	r.key = fmt.Sprintf("%s%x", keyPrefix, sum.Sum(nil))
	return r, nil
}

// UntrustedData is the JSON object the reader reads as data.
func (r Request) UntrustedData() string { return r.data }

// Key is the idempotency key: a digest of the material, the data and the attempt, so a retry of the same request is
// replayed by the Hub without a second charge, and any change of the data is a new key.
func (r Request) Key() string { return r.key }

// IsZero reports whether r is the zero value.
func (r Request) IsZero() bool { return r.data == "" }

// Candidate reports whether id was sent as a candidate.
func (r Request) Candidate(id shared.SubjectID) bool { return r.candidates[id] }
