package thread

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Chat titles (ADR-0028): «Новый чат» until the first text, then that text cut on a word boundary; a rename is 1..80
// characters on one line.
const (
	DefaultTitle   = "Новый чат"
	MaxTitleRunes  = 80
	autoTitleRunes = 40
)

// checkTitle refuses control characters, line separators and the bidi embeddings, overrides and isolates
// (U+202A–U+202E, U+2066–U+2069) that would reorder the chat list.
func checkTitle(s string) error {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxTitleRunes {
		return fmt.Errorf("%w: a title has 1..%d characters", ErrInvalidTitle, MaxTitleRunes)
	}
	for _, r := range s {
		if !titleRune(r) {
			return fmt.Errorf("%w: a title is one line without control or bidi characters", ErrInvalidTitle)
		}
	}
	return nil
}

func titleRune(r rune) bool {
	switch {
	case r == utf8.RuneError, unicode.IsControl(r), r == '\u2028', r == '\u2029':
		return false
	case r >= '\u202A' && r <= '\u202E', r >= '\u2066' && r <= '\u2069':
		return false
	}
	return true
}

// autoTitle is the title from the student's first text: characters a title refuses and every run of spaces become one
// space, then the text is cut to 40 characters at the last space (a single longer word is cut). "" if nothing is left.
func autoTitle(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || !titleRune(r) })
	var b strings.Builder
	n := 0
	for _, w := range words {
		wn := utf8.RuneCountInString(w)
		if n > 0 {
			if n+1+wn > autoTitleRunes {
				break
			}
			b.WriteByte(' ')
			n++
		} else if wn > autoTitleRunes {
			return string([]rune(w)[:autoTitleRunes])
		}
		b.WriteString(w)
		n += wn
	}
	return b.String()
}

// Title returns the chat title.
func (th *Thread) Title() string { return th.title }

// LastActive returns when the last request of the chat started (its creation before the first).
func (th *Thread) LastActive() time.Time { return th.lastActive }

// ArchivedAt returns when the chat was archived; zero when it is not.
func (th *Thread) ArchivedAt() time.Time { return th.archivedAt }

// Archived reports whether the chat is in the archive.
func (th *Thread) Archived() bool { return !th.archivedAt.IsZero() }

// IsEmpty reports whether the chat has no request yet: «Новый чат» returns an empty current chat instead of a new one.
func (th *Thread) IsEmpty() bool { return th.turns == 0 }

// Rename sets the title the student typed, trimmed.
func (th *Thread) Rename(title string, now time.Time) error {
	title = strings.TrimSpace(title)
	if err := checkTitle(title); err != nil {
		return err
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	th.title, th.updatedAt = title, now
	return nil
}

// Archive moves the chat to «Архив» (never deleted: journal lines reference the thread). Only a chat whose current
// request is final or that has none: a running or waiting request is ErrInvalidTransition. Archiving again changes
// nothing.
func (th *Thread) Archive(now time.Time) error {
	if t := th.current; t != nil && !t.final() {
		return fmt.Errorf("%w: archive while the request is %s", ErrInvalidTransition, t.status)
	}
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if th.Archived() {
		return nil
	}
	th.archivedAt, th.updatedAt = now, now
	return nil
}

// Restore is «Вернуть»: the chat leaves the archive. A chat not in the archive is left as is. A new request in an
// archived chat restores it too (StartTurn).
func (th *Thread) Restore(now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: no time", ErrInvalidThread)
	}
	if !th.Archived() {
		return nil
	}
	th.archivedAt, th.updatedAt = time.Time{}, now
	return nil
}
