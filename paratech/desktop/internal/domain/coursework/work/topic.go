package work

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Limits of a topic (TASK-187).
const (
	// MaxTopicTextRunes bounds the text of a topic, in characters after the clean-up.
	MaxTopicTextRunes = 500
	// MaxTopicRequestBytes bounds the key of the request that asked for a work on a topic.
	MaxTopicRequestBytes = 128

	lineSeparator      = rune(0x2028)
	paragraphSeparator = rune(0x2029)
)

var (
	// ErrInvalidTopic means a topic is empty or too long, has a line break or a control character, or its request
	// key or subject is malformed.
	ErrInvalidTopic = errors.New("work: invalid topic")

	// ErrNoAssignment means the work has no assignment of the homework list (an uploaded file or a topic): it is
	// written and exported, never sent to a teacher.
	ErrNoAssignment = errors.New("work: the work has no assignment")
)

// Topic is the source of a work without an assignment: a topic the student (or the agent for the student) asked for,
// with an optional library subject. The text is untrusted data: the reader gets it as the assignment text, never as
// an instruction (ADR-0022). Request is the key of the explicit action that asked for the work (the agent's call, a
// press of the student): the same request finds its work again, another request gets a new work even for the same
// text, so two requests are never merged. Build it with NewTopic.
type Topic struct {
	request string
	text    string
	subject shared.SubjectID
}

// NewTopic cleans and checks a topic: invisible format characters (bidi controls, zero-width) are dropped, runs of
// spaces collapse, the ends are trimmed; an empty text, one longer than MaxTopicTextRunes, a line break or another
// control character is refused. The request key is 1..MaxTopicRequestBytes visible ASCII characters; subject may be
// empty (no subject). Error: ErrInvalidTopic.
func NewTopic(request, text string, subject shared.SubjectID) (Topic, error) {
	clean, ok := cleanTopic(text)
	switch {
	case !ok:
		return Topic{}, fmt.Errorf("%w: the text is empty, too long or has a control character", ErrInvalidTopic)
	case !validRequest(request):
		return Topic{}, fmt.Errorf("%w: request key", ErrInvalidTopic)
	case subject != "" && subject.Validate() != nil:
		return Topic{}, fmt.Errorf("%w: subject", ErrInvalidTopic)
	}
	return Topic{request: request, text: clean, subject: subject}, nil
}

// Request returns the key of the request that asked for the work.
func (t Topic) Request() string { return t.request }

// Text returns the topic, one clean line.
func (t Topic) Text() string { return t.text }

// Subject returns the library subject of the work; "" none.
func (t Topic) Subject() shared.SubjectID { return t.subject }

// IsZero reports whether no topic is set.
func (t Topic) IsZero() bool { return t == Topic{} }

// valid checks a topic as NewTopic leaves it, so a stored one passes the same rules.
func (t Topic) valid() bool {
	clean, ok := cleanTopic(t.text)
	return ok && clean == t.text && validRequest(t.request) && (t.subject == "" || t.subject.Validate() == nil)
}

// cleanTopic drops the format characters, collapses the spaces and checks the result; ok is false for invalid UTF-8,
// a control character (a line break included), an empty text or one longer than MaxTopicTextRunes.
func cleanTopic(s string) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r) || r == lineSeparator || r == paragraphSeparator:
			return "", false
		case unicode.Is(unicode.Cf, r):
			continue // bidi controls and zero-width characters would hide or reorder what the student sees
		case unicode.IsSpace(r):
			r = ' '
		}
		b.WriteRune(r)
	}
	clean := strings.Join(strings.Fields(b.String()), " ")
	if clean == "" || utf8.RuneCountInString(clean) > MaxTopicTextRunes {
		return "", false
	}
	return clean, true
}

func validRequest(s string) bool {
	if s == "" || len(s) > MaxTopicRequestBytes {
		return false
	}
	for i := range len(s) {
		if s[i] < '!' || s[i] > '~' {
			return false
		}
	}
	return true
}

// Sendable reports whether the work may be sent to a teacher: only a work of an assignment of the homework list
// answers one; a work of an uploaded file or of a topic is written and exported only (ErrNoAssignment).
func (w *Work) Sendable() error {
	if w.source.Assignment == "" {
		return ErrNoAssignment
	}
	return nil
}
