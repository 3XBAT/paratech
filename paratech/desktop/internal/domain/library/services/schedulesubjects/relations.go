package schedulesubjects

import (
	"slices"
	"strings"
	"unicode"
)

// The doubtful links between two keys (values.Key of names without their marks). Both only propose: the student
// confirms or splits every doubtful member.

// conjunction is the word an abbreviation may skip: «ТВиМС» and «ТВМС» both stand for «Теория вероятностей и
// математическая статистика».
const conjunction = "и"

// words splits a key into its words: letters and digits, the rest separates.
func words(key string) []string {
	return strings.FieldsFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// compact is the key without spaces and punctuation.
func compact(key string) []rune { return []rune(strings.Join(words(key), "")) }

// related tells how a is doubtfully linked to b, in either direction.
func related(a, b string) (Link, bool) {
	switch {
	case a == b:
		return LinkSame, true
	case isShortForm(a, b) || isShortForm(b, a):
		return LinkAbbreviation, true
	case isTypo(a, b):
		return LinkTypo, true
	}
	return 0, false
}

// isShortForm reports whether short abbreviates long, a name of at least two words besides «и»: either the first
// letters of its words («ТВиМС», «ИЯ»), or prefixes of at least two letters of each of its words in order
// («Матан», «Ин. яз.», «Физкультура» for «Физическая культура»). A short form is strictly shorter.
func isShortForm(short, long string) bool {
	if !sameNumerals(short, long) {
		return false
	}
	var ws [][]rune
	significant := 0
	for _, w := range words(long) {
		ws = append(ws, []rune(w))
		if w != conjunction {
			significant++
		}
	}
	s := compact(short)
	if significant < 2 || len(s) < 2 || len(s) >= len(compact(long)) {
		return false
	}
	return initials(s, ws) || prefixes(s, ws)
}

func initials(s []rune, ws [][]rune) bool {
	if len(ws) == 0 {
		return len(s) == 0
	}
	if string(ws[0]) == conjunction && initials(s, ws[1:]) {
		return true
	}
	return len(s) > 0 && s[0] == ws[0][0] && initials(s[1:], ws[1:])
}

func prefixes(s []rune, ws [][]rune) bool {
	if len(ws) == 0 {
		return len(s) == 0
	}
	if string(ws[0]) == conjunction {
		return prefixes(s, ws[1:])
	}
	for n := 2; n <= len(ws[0]) && n <= len(s); n++ {
		if slices.Equal(s[:n], ws[0][:n]) && prefixes(s[n:], ws[1:]) {
			return true
		}
	}
	return false
}

// typoPrefix is how many first letters of a word a typo never touches: a different start is a different word
// («Микроэкономика» and «Макроэкономика», «Органическая химия» and «Неорганическая химия»).
const typoPrefix = 3

// isTypo reports whether a and b differ by a typo: only by spaces and punctuation, or, word by word, by at most one
// edit for keys of 5 to 8 letters and two for longer ones (the shorter key counts), never in the first typoPrefix
// letters of a word.
func isTypo(a, b string) bool {
	if a == b || !sameNumerals(a, b) {
		return false
	}
	ca, cb := compact(a), compact(b)
	if slices.Equal(ca, cb) {
		return len(ca) > 0
	}

	limit := 0
	switch n := min(len(ca), len(cb)); {
	case n > 8:
		limit = 2
	case n >= 5:
		limit = 1
	}
	wa, wb := words(a), words(b)
	if limit == 0 || len(wa) != len(wb) {
		return false
	}
	total := 0
	for i := range wa {
		if wa[i] == wb[i] {
			continue
		}
		ra, rb := []rune(wa[i]), []rune(wb[i])
		if len(ra) < typoPrefix || len(rb) < typoPrefix || !slices.Equal(ra[:typoPrefix], rb[:typoPrefix]) {
			return false
		}
		if total += levenshtein(ra, rb); total > limit {
			return false
		}
	}
	return total > 0
}

// sameNumerals reports whether a and b have the same numbers (digits or Roman numerals in Latin letters): «Физика 1»
// and «Физика 2» are two subjects.
func sameNumerals(a, b string) bool { return slices.Equal(numerals(a), numerals(b)) }

func numerals(key string) []string {
	var out []string
	for _, w := range words(key) {
		if strings.IndexFunc(w, unicode.IsDigit) >= 0 || strings.Trim(w, "ivxl") == "" {
			out = append(out, w)
		}
	}
	return out
}

func levenshtein(a, b []rune) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
