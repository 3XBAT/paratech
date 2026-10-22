package inspection

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/domain/shared"
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

// Valid reports whether k is one of the kinds.
func (k DocKind) Valid() bool { return slices.Contains(DocKinds(), k) }

// Answer is a checked reader answer: suggestions only. Subject is "" and LessonDate zero when the reader gave none.
type Answer struct {
	DocKind       DocKind
	Subject       shared.SubjectID
	LessonDate    time.Time // a calendar day at 00:00 UTC
	HasHomework   bool
	HasTicketList bool
	MentionsEmail bool
}

// Keys of the answer.
const (
	keyDocKind       = "doc_kind"
	keySubject       = "subject"
	keyLessonDate    = "lesson_date"
	keyHasHomework   = "has_homework"
	keyHasTicketList = "has_ticket_list"
	keyMentionsEmail = "mentions_email"
)

func answerKeys() []string {
	return []string{keyDocKind, keySubject, keyLessonDate, keyHasHomework, keyHasTicketList, keyMentionsEmail}
}

// Parse checks the reader's output against the schema: one JSON object with exactly the six keys once each,
// doc_kind one of DocKinds, subject null or one of the candidates of r, lesson_date null or a date YYYY-MM-DD, three
// booleans. Anything else (another key, a repeated key, free text in a field, a second value, a code fence) refuses
// the whole answer with ErrInvalidAnswer; the error never quotes the output.
func (r Request) Parse(output string) (Answer, error) {
	var a Answer
	seen := map[string]bool{}
	d := json.NewDecoder(strings.NewReader(output))
	d.UseNumber()
	err := readObject(d, func(key string) error {
		if seen[key] || !slices.Contains(answerKeys(), key) {
			return fmt.Errorf("%w: unknown or repeated key", ErrInvalidAnswer)
		}
		seen[key] = true
		var ok bool
		switch key {
		case keyDocKind:
			var s string
			s, ok = readString(d)
			a.DocKind = DocKind(s)
			ok = ok && a.DocKind.Valid()
		case keySubject:
			var s string
			var present bool
			s, present, ok = readNullable(d)
			a.Subject = shared.SubjectID(s)
			ok = ok && (!present || r.Candidate(a.Subject))
		case keyLessonDate:
			var s string
			var present bool
			s, present, ok = readNullable(d)
			if ok && present {
				a.LessonDate, ok = parseDate(s)
			}
		case keyHasHomework:
			a.HasHomework, ok = readBool(d)
		case keyHasTicketList:
			a.HasTicketList, ok = readBool(d)
		case keyMentionsEmail:
			a.MentionsEmail, ok = readBool(d)
		}
		if !ok {
			return fmt.Errorf("%w: %s is not a value of the schema", ErrInvalidAnswer, key)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalidAnswer) {
			return Answer{}, err
		}
		return Answer{}, fmt.Errorf("%w: %w", ErrInvalidAnswer, err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return Answer{}, fmt.Errorf("%w: data after the object", ErrInvalidAnswer)
	}
	for _, k := range answerKeys() {
		if !seen[k] {
			return Answer{}, fmt.Errorf("%w: %s is missing", ErrInvalidAnswer, k)
		}
	}
	return a, nil
}

// parseDate reads exactly YYYY-MM-DD of a real day.
func parseDate(s string) (time.Time, bool) {
	if len(s) != len(time.DateOnly) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

// errJSON is the cause of a refusal for the shape of the JSON; the caller wraps it with ErrInvalidAnswer.
var errJSON = errors.New("not one JSON object of the schema")

// readObject reads the next value of d as an object and calls field for each key with d at its value; field must
// consume the value. Keys match byte for byte.
func readObject(d *json.Decoder, field func(key string) error) error {
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return errJSON
	}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return errJSON
		}
		key, ok := t.(string)
		if !ok {
			return errJSON
		}
		if err := field(key); err != nil {
			return err
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') {
		return errJSON
	}
	return nil
}

func readString(d *json.Decoder) (string, bool) {
	t, err := d.Token()
	if err != nil {
		return "", false
	}
	s, ok := t.(string)
	return s, ok
}

// readNullable reads a string or null (present false).
func readNullable(d *json.Decoder) (s string, present, ok bool) {
	t, err := d.Token()
	if err != nil {
		return "", false, false
	}
	if t == nil {
		return "", false, true
	}
	s, ok = t.(string)
	return s, true, ok
}

func readBool(d *json.Decoder) (bool, bool) {
	t, err := d.Token()
	if err != nil {
		return false, false
	}
	b, ok := t.(bool)
	return b, ok
}
