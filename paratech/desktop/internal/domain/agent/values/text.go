package values

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Length limits in characters (runes).
const (
	MaxTextRunes      = 300  // card text from show_plan, cannot_do, ask_questions
	MaxReplyTextRunes = 4000 // reply.text (catalog v7, TASK-223)
	MaxLabelRunes     = 80   // label from the student's request and names
	MaxQueryRunes     = 500  // the student's question or search text (since catalog v2)
)

// Text is model text for a card: at most 300 characters, printable, no HTML angle brackets; line breaks allowed. The
// text of a reply card is a reply text (ParseReplyText). It is shown to the student only and never sent back as an
// instruction.
type Text string

// ParseText trims s and checks it.
func ParseText(s string) (Text, error) {
	s = strings.TrimSpace(s)
	if err := checkPrintable(s, MaxTextRunes, true, false); err != nil {
		return "", err
	}
	return Text(s), nil
}

// ParseReplyText trims s and checks it as the text of a reply card: at most MaxReplyTextRunes printable characters and
// line breaks. Angle brackets are allowed: the card renders the text as plain text only (ADR-0023, TASK-223).
func ParseReplyText(s string) (Text, error) {
	s = strings.TrimSpace(s)
	if err := checkPrintable(s, MaxReplyTextRunes, true, true); err != nil {
		return "", err
	}
	return Text(s), nil
}

// Label is a short single-line text from the student's request (search query, instruction, question).
type Label string

// ParseLabel trims s and checks it: 1..80 printable characters, one line, no HTML angle brackets.
func ParseLabel(s string) (Label, error) {
	s = strings.TrimSpace(s)
	if err := checkPrintable(s, MaxLabelRunes, false, false); err != nil {
		return "", err
	}
	return Label(s), nil
}

// Query is the student's question or search text in their own words (answer_question.question, search.query).
type Query string

// ParseQuery trims s and checks it as a Label of up to 500 characters: printable, one line, no HTML angle brackets.
func ParseQuery(s string) (Query, error) {
	s = strings.TrimSpace(s)
	if err := checkPrintable(s, MaxQueryRunes, false, false); err != nil {
		return "", err
	}
	return Query(s), nil
}

// Name is model text that persists as a subject, folder or semester name and is later shown to the planner as data
// (ADR-0022): 1..80 characters of letters, digits, single spaces and ".,-()", and a valid Windows file-name segment
// (no trailing dot, no device name), because a folder name may become a folder on export.
type Name string

// nameExtraRunes are the non-alphanumeric characters a Name may hold (ADR-0022).
const nameExtraRunes = " .,-()"

// ParseName trims s and checks it.
func ParseName(s string) (Name, error) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxLabelRunes {
		return "", fmt.Errorf("%w: a name must have 1..%d characters", ErrInvalidText, MaxLabelRunes)
	}
	for _, r := range s {
		if r == utf8.RuneError || !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(nameExtraRunes, r) {
			return "", fmt.Errorf("%w: a name may hold only letters, digits, spaces and .,-()", ErrInvalidText)
		}
	}
	if strings.Contains(s, "  ") {
		return "", fmt.Errorf("%w: a name has repeated spaces", ErrInvalidText)
	}
	if why := shared.NameSegmentProblem(s); why != "" {
		return "", fmt.Errorf("%w: a name %s", ErrInvalidText, why)
	}
	return Name(s), nil
}

// checkPrintable allows letters, marks, digits, punctuation, symbols and the ASCII space (unicode.IsPrint), so
// control, format (bidi overrides, zero-width), separator and private-use characters are refused. Angle brackets only
// with angles.
func checkPrintable(s string, maxRunes int, newlines, angles bool) error {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return fmt.Errorf("%w: must have 1..%d characters", ErrInvalidText, maxRunes)
	}
	for _, r := range s {
		if r == '\n' && newlines {
			continue
		}
		if !unicode.IsPrint(r) {
			return fmt.Errorf("%w: has a control, format or separator character", ErrInvalidText)
		}
		// JSON decoding turns invalid UTF-8 into U+FFFD, so it marks broken input.
		if r == utf8.RuneError {
			return fmt.Errorf("%w: has a replacement character", ErrInvalidText)
		}
		if !angles && (r == '<' || r == '>') {
			return fmt.Errorf("%w: has an angle bracket (no HTML)", ErrInvalidText)
		}
	}
	return nil
}
