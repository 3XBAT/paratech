// Package citation checks the quotes of an answer from the student's lessons (ADR-0022, TASK-118): a quote is
// evidence only if it occurs in the text of the fragment it cites. Models copy loosely, so both are read the same
// forgiving way before the search: case-insensitively, «ё» as «е», dashes as one dash, every run of spaces, line
// breaks and invisible characters as one space, punctuation at the ends of the quote dropped. Nothing else is
// forgiven: a paraphrase is not a quote. Pure, no I/O.
package citation

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MinQuoteRunes is the shortest quote, after Normalize, that counts as evidence: a word or two of a lesson proves
// nothing about a claim.
const MinQuoteRunes = 8

// Normalize reads s as quotes are matched: lower case, «ё» as «е», the dashes ‐‑‒–—― and the minus as "-", format
// characters (zero-width, bidi) removed, any run of spaces and control characters as one space, the ends trimmed.
// Invalid UTF-8 becomes U+FFFD, which matches only itself.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsSpace(r) || unicode.IsControl(r):
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(fold(r))
	}
	return b.String()
}

// fold maps one rune of a normalized text.
func fold(r rune) rune {
	switch r {
	case 'ё', 'Ё':
		return 'е'
	case '‐', '‑', '‒', '–', '—', '―', '−':
		return '-'
	}
	return unicode.ToLower(r)
}

// Quote is the quote as it is searched: Normalize with the punctuation and spaces at its ends dropped (a model adds
// «…», quotation marks or a final period that the lesson text may not have there).
func Quote(quote string) string {
	return strings.TrimFunc(Normalize(quote), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
}

// Matches reports whether quote occurs in text, both read as above, and is long enough to be evidence (at least
// MinQuoteRunes characters after Quote).
func Matches(quote, text string) bool {
	q := Quote(quote)
	if utf8.RuneCountInString(q) < MinQuoteRunes {
		return false
	}
	return strings.Contains(Normalize(text), q)
}
