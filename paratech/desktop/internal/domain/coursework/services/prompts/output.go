package prompts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/values"
)

// Limits of the answers. The rune limits are the maxLength and maxItems of the schemas (schema_test.go checks it).
const (
	MaxOutputBytes      = 256 << 10 // the whole model answer
	MaxSummaryRunes     = 20000
	MaxKeyPoints        = 20
	MaxKeyPointRunes    = 300
	MaxHomework         = 20 // items of one extraction
	MaxTitleRunes       = 200
	MaxDescriptionRunes = 4000
	MaxDraftBodyRunes   = 16000 // keeps a draft below proposal.MaxBodyBytes even in 4-byte runes

	// A deadline outside [lesson start - MaxDueBefore, lesson start + MaxDueAhead] is a model error.
	MaxDueBefore = 24 * time.Hour
	MaxDueAhead  = 2 * 365 * 24 * time.Hour
)

// Summary is the validated answer of the summary operation.
type Summary struct {
	text      string
	keyPoints []string
}

// Text returns the summary text. It is model-written data, never an instruction.
func (s Summary) Text() string { return s.text }

// KeyPoints returns the main ideas, one line each.
func (s Summary) KeyPoints() []string { return append([]string(nil), s.keyPoints...) }

// Homework is one assignment the model found in a transcript. It is a proposal only: the caller stores it for the
// student to review and never acts on it by itself (hard rule 3).
type Homework struct {
	title       string
	description string
	due         time.Time
}

// Title returns the one-line title.
func (h Homework) Title() string { return h.title }

// Description returns the description; may be empty.
func (h Homework) Description() string { return h.description }

// Due returns the deadline in UTC; zero when the teacher named none.
func (h Homework) Due() time.Time { return h.due }

// Extraction is the validated answer of the extract_homework_deadlines operation; it may hold no homework.
type Extraction struct {
	homework []Homework
}

// Homework returns the assignments found, in the order of the answer.
func (e Extraction) Homework() []Homework { return append([]Homework(nil), e.homework...) }

// Draft is the validated answer of the homework_draft operation. It holds text only: where the homework goes is
// decided by the core from local state, never by the model.
type Draft struct {
	title string
	body  string
}

// Title returns the one-line title of the draft.
func (d Draft) Title() string { return d.title }

// Body returns the homework text.
func (d Draft) Body() string { return d.body }

// Wire shapes of the answers. Pointers tell a missing field from an empty one; every field is required.
type summaryWire struct {
	Summary   *string   `json:"summary"`
	KeyPoints *[]string `json:"key_points"`
}

type extractWire struct {
	Assignments *[]homeworkWire `json:"assignments"`
}

type homeworkWire struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Due         nullable `json:"due"`
}

type draftWire struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

// nullable is a required field whose value is a string or null.
type nullable struct {
	present bool
	value   *string
}

// UnmarshalJSON is called for null too, so a present null is told from a missing field.
func (n *nullable) UnmarshalJSON(b []byte) error {
	n.present = true
	if string(b) == "null" {
		n.value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n.value = &s
	return nil
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedOutput, fmt.Sprintf(format, args...))
}

// ParseSummary checks the answer of a summary request: summary (1 to MaxSummaryRunes) and key_points (at most
// MaxKeyPoints lines of 1 to MaxKeyPointRunes). Errors wrap ErrMalformedOutput.
func ParseSummary(out values.UntrustedOutput) (Summary, error) {
	var w summaryWire
	if err := decode(out, &w, "summary", "key_points"); err != nil {
		return Summary{}, err
	}
	if w.Summary == nil || w.KeyPoints == nil {
		return Summary{}, malformed("summary and key_points are required")
	}

	text, err := checkText("summary", *w.Summary, MaxSummaryRunes, false)
	if err != nil {
		return Summary{}, err
	}
	if len(*w.KeyPoints) > MaxKeyPoints {
		return Summary{}, malformed("more than %d key_points", MaxKeyPoints)
	}
	points := make([]string, 0, len(*w.KeyPoints))
	for i, p := range *w.KeyPoints {
		line, err := checkLine(fmt.Sprintf("key_points[%d]", i), p, MaxKeyPointRunes)
		if err != nil {
			return Summary{}, err
		}
		points = append(points, line)
	}
	return Summary{text: text, keyPoints: points}, nil
}

// ParseExtraction checks the answer of an extract_homework_deadlines request: at most MaxHomework assignments, each
// with a one-line title, a description (may be empty) and a due that is null or an RFC 3339 time within
// [lessonStart - MaxDueBefore, lessonStart + MaxDueAhead]. lessonStart is the one the request was built with; a zero
// one wraps ErrNoContext. Other errors wrap ErrMalformedOutput.
func ParseExtraction(out values.UntrustedOutput, lessonStart time.Time) (Extraction, error) {
	if lessonStart.IsZero() {
		return Extraction{}, fmt.Errorf("%w: no lesson start to check deadlines against", ErrNoContext)
	}

	var w extractWire
	if err := decode(out, &w, "assignments", "title", "description", "due"); err != nil {
		return Extraction{}, err
	}
	if w.Assignments == nil {
		return Extraction{}, malformed("assignments is required")
	}
	if len(*w.Assignments) > MaxHomework {
		return Extraction{}, malformed("more than %d assignments", MaxHomework)
	}

	items := make([]Homework, 0, len(*w.Assignments))
	for i, a := range *w.Assignments {
		h, err := checkHomework(a, lessonStart)
		if err != nil {
			return Extraction{}, fmt.Errorf("assignments[%d]: %w", i, err)
		}
		items = append(items, h)
	}
	return Extraction{homework: items}, nil
}

func checkHomework(a homeworkWire, lessonStart time.Time) (Homework, error) {
	if a.Title == nil || a.Description == nil || !a.Due.present {
		return Homework{}, malformed("title, description and due are required")
	}

	var h Homework
	var err error
	if h.title, err = checkLine("title", *a.Title, MaxTitleRunes); err != nil {
		return Homework{}, err
	}
	if h.description, err = checkText("description", *a.Description, MaxDescriptionRunes, true); err != nil {
		return Homework{}, err
	}
	if a.Due.value != nil {
		due, err := time.Parse(time.RFC3339, *a.Due.value)
		if err != nil {
			return Homework{}, malformed("due is not an RFC 3339 time")
		}
		if due.Before(lessonStart.Add(-MaxDueBefore)) || due.After(lessonStart.Add(MaxDueAhead)) {
			return Homework{}, malformed("due is out of range of the lesson date")
		}
		h.due = due.UTC()
	}
	return h, nil
}

// ParseDraft checks the answer of a homework_draft request: a one-line title and a body of 1 to MaxDraftBodyRunes.
// Nothing else is accepted, in particular no recipient, address, path or calendar field. Errors wrap
// ErrMalformedOutput.
func ParseDraft(out values.UntrustedOutput) (Draft, error) {
	var w draftWire
	if err := decode(out, &w, "title", "body"); err != nil {
		return Draft{}, err
	}
	if w.Title == nil || w.Body == nil {
		return Draft{}, malformed("title and body are required")
	}

	title, err := checkLine("title", *w.Title, MaxTitleRunes)
	if err != nil {
		return Draft{}, err
	}
	body, err := checkText("body", *w.Body, MaxDraftBodyRunes, false)
	if err != nil {
		return Draft{}, err
	}
	return Draft{title: title, body: body}, nil
}

// decode reads exactly one JSON object of at most MaxOutputBytes into v. Beyond DisallowUnknownFields it requires
// object keys to be exactly one of names (encoding/json matches keys case-insensitively) and unique (it keeps the
// last duplicate, which would hide text from whoever reads the first).
func decode(out values.UntrustedOutput, v any, names ...string) error {
	text := out.Unvalidated()
	if len(text) > MaxOutputBytes {
		return malformed("output is larger than %d bytes", MaxOutputBytes)
	}
	if !utf8.ValidString(text) {
		return malformed("output is not valid UTF-8")
	}
	if err := checkKeys(text, names); err != nil {
		return err
	}

	dec := json.NewDecoder(strings.NewReader(text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// The decoder error may quote model text; keep only the class.
		return malformed("not a single JSON object of the schema")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return malformed("trailing data after the JSON object")
	}
	return nil
}

// checkKeys walks the tokens of text and rejects an object key that is not exactly one of names or repeats within
// its object. Structure and types are left to the decoder.
func checkKeys(text string, names []string) error {
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}

	type frame struct {
		object    bool
		keys      map[string]bool
		expectKey bool
	}
	var stack []*frame
	dec := json.NewDecoder(strings.NewReader(text))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return malformed("not valid JSON")
		}

		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if s, ok := tok.(string); ok && top != nil && top.object && top.expectKey {
			if !known[s] {
				return malformed("unknown field")
			}
			if top.keys[s] {
				return malformed("duplicate field %s", s)
			}
			top.keys[s] = true
			top.expectKey = false
			continue
		}

		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:len(stack)-1]
			continue
		}
		// A value: the next token of the enclosing object is a key again (after the nested value closes, if any).
		if top != nil && top.object {
			top.expectKey = true
		}
		if d, ok := tok.(json.Delim); ok {
			stack = append(stack, &frame{object: d == '{', keys: map[string]bool{}, expectKey: true})
		}
	}
}

// checkLine accepts one line of 1 to maxRunes characters after trimming.
func checkLine(field, s string, maxRunes int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxRunes || strings.ContainsFunc(s, unsafeInLine) {
		return "", malformed("%s must be one line of 1-%d characters without control characters", field, maxRunes)
	}
	return s, nil
}

// checkText accepts multi-line text of at most maxRunes characters after trimming; empty only when allowEmpty.
func checkText(field, s string, maxRunes int, allowEmpty bool) (string, error) {
	s = strings.TrimSpace(s)
	if (s == "" && !allowEmpty) || utf8.RuneCountInString(s) > maxRunes || strings.ContainsFunc(s, unsafeInText) {
		return "", malformed("%s must be text of at most %d characters without control characters", field, maxRunes)
	}
	return s, nil
}

func unsafeInLine(r rune) bool { return unicode.IsControl(r) || bidiControl(r) }

func unsafeInText(r rune) bool {
	return (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') || bidiControl(r)
}

// bidiControl reports the embedding, override and isolate controls, which can make displayed text differ from what
// it is (the "Trojan Source" trick); ordinary text does not need them.
func bidiControl(r rune) bool { return (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩') }
