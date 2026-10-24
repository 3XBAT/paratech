package ticketset

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/coursework/answer"
)

// Bounds of a ticket list.
const (
	// MaxTickets bounds the tickets of one set, and so of one paid run.
	MaxTickets = 100
	// MaxTitleRunes bounds one ticket: it is asked as one question (answer.MaxQuestionRunes).
	MaxTitleRunes = answer.MaxQuestionRunes
	// MaxListBytes bounds the list text read (a pasted text or the text of a material).
	MaxListBytes = 512 << 10
	// MaxNumber bounds a ticket number of the list.
	MaxNumber = 999

	ticketWord = "билет"
)

// Item is a ticket as the list gives it: its number and its title on one line.
type Item struct {
	Number int
	Title  string
}

// ParseList reads the tickets of a list text deterministically; nothing in the text is an instruction (ADR-0022).
//
// A ticket starts on a line «Билет N» (any case, «№» allowed, then «.», «)», «:» or a dash) or, when the text has no
// such line, on a numbered line «N.» or «N)». The text after the number and the lines that follow it, up to the next
// ticket, make its title on one line (in the numbered form a blank line ends it, so the text after the list is not
// taken). Lines before the first ticket are ignored, a ticket without text is dropped. Control and invisible format
// characters are removed, spaces collapsed. Numbers that do not rise strictly are replaced by 1..n in order.
//
// Errors: ErrListTooLong over MaxListBytes, ErrNoTickets, ErrTooManyTickets over MaxTickets, ErrTitleTooLong for a
// ticket over MaxTitleRunes.
func ParseList(text string) ([]Item, error) {
	if len(text) > MaxListBytes {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrListTooLong, MaxListBytes)
	}
	lines := strings.Split(clean(text), "\n")
	byWord := false
	for _, l := range lines {
		if _, _, ok := wordStart(collapse(l)); ok {
			byWord = true
			break
		}
	}

	var items []Item
	open := false
	for _, raw := range lines {
		line := collapse(raw)
		if line == "" {
			open = open && byWord
			continue
		}
		n, rest, ok := wordStart(line)
		if !byWord {
			n, rest, ok = numberStart(line)
		}
		switch {
		case ok:
			if len(items) == MaxTickets {
				return nil, fmt.Errorf("%w: more than %d", ErrTooManyTickets, MaxTickets)
			}
			items = append(items, Item{Number: n, Title: rest})
			open = true
		case open:
			last := &items[len(items)-1]
			last.Title = strings.TrimSpace(last.Title + " " + line)
			if utf8.RuneCountInString(last.Title) > MaxTitleRunes {
				return nil, fmt.Errorf("%w: ticket %d", ErrTitleTooLong, len(items))
			}
		}
	}

	out := make([]Item, 0, len(items))
	for _, it := range items {
		if it.Title != "" {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoTickets
	}
	rising := true
	for i := range out {
		if i > 0 && out[i].Number <= out[i-1].Number {
			rising = false
		}
	}
	for i := range out {
		if !rising {
			out[i].Number = i + 1
		}
		if utf8.RuneCountInString(out[i].Title) > MaxTitleRunes {
			return nil, fmt.Errorf("%w: ticket %d", ErrTitleTooLong, out[i].Number)
		}
	}
	return out, nil
}

// clean keeps valid UTF-8 with "\n" line breaks: other spaces become " ", control and format characters (zero-width,
// bidi) are removed.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}

// collapse trims a line and collapses its runs of spaces.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// wordStart reads «Билет N» at the start of a line: the number and the text after it.
func wordStart(line string) (int, string, bool) {
	if len(line) < len(ticketWord) || !strings.EqualFold(line[:len(ticketWord)], ticketWord) {
		return 0, "", false
	}
	rest := strings.TrimLeft(line[len(ticketWord):], " ")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "№"), " ")
	n, rest, ok := number(rest)
	if !ok {
		return 0, "", false
	}
	if rest != "" && rest[0] != ' ' && !separated(rest) {
		return 0, "", false // «Билет 1а»: not a ticket number
	}
	rest = strings.TrimLeft(rest, " ")
	if separated(rest) {
		_, size := utf8.DecodeRuneInString(rest)
		rest = rest[size:]
	}
	return n, strings.TrimSpace(rest), true
}

// separated reports whether s starts with a separator after a ticket number.
func separated(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return s != "" && strings.ContainsRune(".):-–—", r)
}

// numberStart reads «N.» or «N)» followed by a space or the end of the line; «1.5» is not a ticket.
func numberStart(line string) (int, string, bool) {
	n, rest, ok := number(line)
	if !ok || rest == "" || (rest[0] != '.' && rest[0] != ')') {
		return 0, "", false
	}
	rest = rest[1:]
	if rest != "" && rest[0] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(rest), true
}

// number reads 1..3 ASCII digits of a number 1..MaxNumber at the start of s.
func number(s string) (int, string, bool) {
	i, n := 0, 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		if i == 3 {
			return 0, "", false
		}
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == 0 || n < 1 || n > MaxNumber {
		return 0, "", false
	}
	return n, s[i:], true
}
