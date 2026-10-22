// Package inspect is the quarantined reader of an uploaded material (OPERATION_TYPE_INSPECT_MATERIAL, ADR-0022): the
// shape of its input and the strict check of its answer. The material text is untrusted data; the answer holds only
// an enum, an ID from the passed candidates, a date and flags, so nothing the text says can become an instruction, an
// address or an action.
//
// Wire shape (CompletionSpec.untrusted_data, no client instructions; CompletionResult.output):
//
//	input:  {"today":"YYYY-MM-DD","subjects":[{"id":"<id>","name":"<name>"}],"material_text":"<text>"}
//	answer: {"doc_kind":"<kind>","subject":"<id>"|null,"lesson_date":"YYYY-MM-DD"|null,
//	         "has_homework":bool,"has_ticket_list":bool,"mentions_email":bool}
package inspect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"paratech/hub/internal/domain/llm/values"
)

// Limits of the input. Sizes are bytes of UTF-8.
const (
	// MaxTextBytes bounds the material text; the desktop sends at most this much of the beginning of a material.
	MaxTextBytes = 128 << 10
	// MaxSubjects bounds the candidate subjects, as many as an agent base may hold.
	MaxSubjects = values.MaxAgentSubjects
)

// FailedOutput is the stored result of a reader answer that was charged but refused, so a retry with the same key
// repeats the refusal instead of paying again. It never passes ParseAnswer.
const FailedOutput = "{}"

// Keys of the input and the answer.
const (
	keyToday         = "today"
	keySubjects      = "subjects"
	keyMaterialText  = "material_text"
	keyID            = "id"
	keyName          = "name"
	keyDocKind       = "doc_kind"
	keySubject       = "subject"
	keyLessonDate    = "lesson_date"
	keyHasHomework   = "has_homework"
	keyHasTicketList = "has_ticket_list"
	keyMentionsEmail = "mentions_email"
)

// Errors of the reader (ARCH-18). None of them quotes the material, a name or the model's answer.
var (
	// ErrInvalidInput means the untrusted data of a request is not a valid reader input.
	ErrInvalidInput = errors.New("invalid inspect_material input")
	// ErrInvalidAnswer means the model's answer breaks the reader schema; the whole answer is refused.
	ErrInvalidAnswer = errors.New("invalid inspect_material answer")
)

// DocKind is the kind of a material. Closed set.
type DocKind string

// Kinds of material.
const (
	DocLecture    DocKind = "lecture"
	DocSeminar    DocKind = "seminar"
	DocLab        DocKind = "lab"
	DocHomework   DocKind = "homework"
	DocTicketList DocKind = "ticket_list"
	DocSyllabus   DocKind = "syllabus"
	DocOther      DocKind = "other"
)

// DocKinds returns every kind, in a fixed order.
func DocKinds() []DocKind {
	return []DocKind{DocLecture, DocSeminar, DocLab, DocHomework, DocTicketList, DocSyllabus, DocOther}
}

// Subject is a candidate subject: the desktop ID the answer may name and its name (data for the model).
type Subject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Input is a checked reader request. Build it with ParseInput.
type Input struct {
	today    string
	subjects []Subject
	text     string
}

// ParseInput checks the untrusted data of a request: one JSON object with exactly the keys today (a date), subjects
// (0-MaxSubjects candidates with distinct valid IDs and names of ADR-0022) and material_text (non-blank, at most
// MaxTextBytes, without NUL).
func ParseInput(data string) (Input, error) {
	var (
		in   Input
		seen = map[string]bool{}
		ids  = map[string]bool{}
	)
	err := decodeObject(data, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyToday:
			in.today, ok = readString(d)
			if !ok || !values.ValidDate(in.today) {
				return fmt.Errorf("%w: today must be a date YYYY-MM-DD", ErrInvalidInput)
			}
		case keyMaterialText:
			in.text, ok = readString(d)
			if !ok || len(in.text) > MaxTextBytes || strings.TrimSpace(in.text) == "" || strings.ContainsRune(in.text, 0) {
				return fmt.Errorf("%w: material text must be 1-%d bytes, not blank, without NUL", ErrInvalidInput, MaxTextBytes)
			}
		case keySubjects:
			in.subjects = []Subject{}
			return readArray(d, func(d *json.Decoder) error {
				s, err := readSubject(d)
				if err != nil {
					return err
				}
				if len(in.subjects) == MaxSubjects || ids[s.ID] {
					return fmt.Errorf("%w: at most %d subjects with distinct ids", ErrInvalidInput, MaxSubjects)
				}
				ids[s.ID] = true
				in.subjects = append(in.subjects, s)
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
	if !seen[keyToday] || !seen[keySubjects] || !seen[keyMaterialText] {
		return Input{}, fmt.Errorf("%w: today, subjects and material_text are required", ErrInvalidInput)
	}
	return in, nil
}

func readSubject(d *json.Decoder) (Subject, error) {
	var s Subject
	seen := map[string]bool{}
	err := readObject(d, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyID:
			s.ID, ok = readString(d)
		case keyName:
			s.Name, ok = readString(d)
		}
		if !ok {
			return fmt.Errorf("%w: a subject is {id, name} of strings", ErrInvalidInput)
		}
		return nil
	})
	if err != nil {
		return Subject{}, err
	}
	if !seen[keyID] || !seen[keyName] || !values.ValidID(s.ID) || !values.ValidSubjectName(s.Name) {
		return Subject{}, fmt.Errorf("%w: a subject needs a valid id and a name of 1-%d letters, digits, spaces or .,-()",
			ErrInvalidInput, values.MaxLabelRunes)
	}
	return s, nil
}

// ModelData is what the model reads: the checked input re-encoded as one JSON object, so the material text is a JSON
// string value and cannot end the data or pose as another key. HTML characters are not escaped, so the encoding is
// never larger than the client's own beyond a few escapes.
func (in Input) ModelData() string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings and a slice of them: encoding cannot fail.
	_ = enc.Encode(struct {
		Today        string    `json:"today"`
		Subjects     []Subject `json:"subjects"`
		MaterialText string    `json:"material_text"`
	}{in.today, in.subjects, in.text})
	return strings.TrimSuffix(b.String(), "\n")
}

// Answer is a checked reader answer. Subject and LessonDate are empty when the model gave none.
type Answer struct {
	DocKind       DocKind
	Subject       string
	LessonDate    string
	HasHomework   bool
	HasTicketList bool
	MentionsEmail bool
}

// ParseAnswer checks the model's output against the schema: exactly the six keys once each, doc_kind one of DocKinds,
// subject null or one of the candidate IDs of in, lesson_date null or a date YYYY-MM-DD, three booleans. Whitespace
// and one Markdown code fence around the object are tolerated; anything else (another key, free text, an address in
// any field, a second object) refuses the whole answer.
func (in Input) ParseAnswer(output string) (Answer, error) {
	body, ok := unfence(output)
	if !ok {
		return Answer{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidAnswer)
	}
	var (
		a    Answer
		seen = map[string]bool{}
	)
	err := decodeObject(body, func(key string, d *json.Decoder) error {
		seen[key] = true
		var ok bool
		switch key {
		case keyDocKind:
			var s string
			s, ok = readString(d)
			ok = ok && slices.Contains(DocKinds(), DocKind(s))
			a.DocKind = DocKind(s)
		case keySubject:
			var present bool
			a.Subject, present, ok = readNullableString(d)
			ok = ok && (!present || in.candidate(a.Subject))
		case keyLessonDate:
			var present bool
			a.LessonDate, present, ok = readNullableString(d)
			ok = ok && (!present || values.ValidDate(a.LessonDate))
		case keyHasHomework:
			a.HasHomework, ok = readBool(d)
		case keyHasTicketList:
			a.HasTicketList, ok = readBool(d)
		case keyMentionsEmail:
			a.MentionsEmail, ok = readBool(d)
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
	for _, k := range []string{keyDocKind, keySubject, keyLessonDate, keyHasHomework, keyHasTicketList, keyMentionsEmail} {
		if !seen[k] {
			return Answer{}, fmt.Errorf("%w: %s is missing", ErrInvalidAnswer, k)
		}
	}
	return a, nil
}

// Encode is the canonical answer the Hub returns and stores: the six keys in a fixed order, null for none.
func (a Answer) Encode() string {
	b, _ := json.Marshal(struct { // only strings, nil pointers and booleans: cannot fail
		DocKind       DocKind `json:"doc_kind"`
		Subject       *string `json:"subject"`
		LessonDate    *string `json:"lesson_date"`
		HasHomework   bool    `json:"has_homework"`
		HasTicketList bool    `json:"has_ticket_list"`
		MentionsEmail bool    `json:"mentions_email"`
	}{a.DocKind, orNull(a.Subject), orNull(a.LessonDate), a.HasHomework, a.HasTicketList, a.MentionsEmail})
	return string(b)
}

func (in Input) candidate(id string) bool {
	for _, s := range in.subjects {
		if s.ID == id {
			return true
		}
	}
	return false
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// unfence trims whitespace and one surrounding Markdown code fence (``` or ```json), which models add despite the
// prompt; what is inside is checked as strictly as without it.
func unfence(output string) (string, bool) {
	if !utf8.ValidString(output) {
		return "", false
	}
	s := strings.TrimSpace(output)
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		rest = strings.TrimPrefix(rest, "json")
		if inner, ok := strings.CutSuffix(rest, "```"); ok && strings.HasPrefix(inner, "\n") {
			s = strings.TrimSpace(inner)
		}
	}
	return s, true
}

// safeKey names a schema key in an error; any other key (model text) is not quoted.
func safeKey(key string) string {
	switch key {
	case keyDocKind, keySubject, keyLessonDate, keyHasHomework, keyHasTicketList, keyMentionsEmail:
		return key
	}
	return "an unknown key"
}
