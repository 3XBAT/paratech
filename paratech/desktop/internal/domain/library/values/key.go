package values

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CleanText turns typed or imported text into one plain line: invalid UTF-8 and format characters are dropped, any
// space or control character becomes a space, runs of spaces collapse and the ends are trimmed.
func CleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError || unicode.Is(unicode.Cf, r):
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

// Key is the matching key of a name: CleanText, lower case and «ё» read as «е», so «Физика», « физика » and
// «ФИЗИКА» are one key. "" means no name.
func Key(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 'ё' || r == 'Ё' {
			return 'е'
		}
		return unicode.ToLower(r)
	}, CleanText(s))
}
