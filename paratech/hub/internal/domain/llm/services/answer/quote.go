package answer

import (
	"strings"
	"unicode"
)

// The quote search is the Hub's copy of the desktop's reading of quotes (desktop coursework/services/citation, which
// stays the final check): case-insensitive, «ё» as «е», dashes as one dash, format characters removed, every run of
// spaces and control characters as one space, punctuation and spaces at the ends of the quote dropped. It must never be
// stricter than the desktop's, or the Hub would drop a citation the desktop accepts. The desktop's minimum quote
// length is not applied here: whether a short quote is evidence is the desktop's call.

// quoteIn reports whether quote occurs in text, a fragment already normalized. One search per citation, bounded by the
// fragment: a quote longer than its fragment is refused before the search.
func quoteIn(quote, text string) bool {
	q := strings.TrimFunc(normalize(quote), func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
	if q == "" || len(q) > len(text) {
		return false
	}
	return strings.Contains(text, q)
}

// normalize reads s as quotes are matched (see above). Invalid UTF-8 becomes U+FFFD, which matches only itself.
func normalize(s string) string {
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
