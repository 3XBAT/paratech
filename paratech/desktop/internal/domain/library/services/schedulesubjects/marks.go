package schedulesubjects

import (
	"cmp"
	"slices"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
)

// kind is the kind of lesson a mark names.
type kind int

const (
	kindLecture kind = iota + 1
	kindSeminar      // a seminar or a practice
	kindLab
)

// mark is a lesson-kind mark a schedule writes next to the subject: «Физика (лек.)», «Матанализ, сем.».
type mark struct {
	text string // in the values.Key form
	kind kind

	// short marks an abbreviation: it may follow the subject after a plain space at the end («Информатика лаб.», a
	// mark of two letters only with its dot) and start it with its dot («Лек. Физика»). A full word needs brackets, a
	// hyphen, a comma or a colon, so «Учебная практика» and «Семинар по физике» stay as they are.
	short bool
}

// marks is the one table of lesson-kind marks.
var marks = []mark{
	{"лек", kindLecture, true},
	{"лекц", kindLecture, true},
	{"лекция", kindLecture, false},
	{"лекции", kindLecture, false},
	{"лекционное занятие", kindLecture, false},
	{"лекционные занятия", kindLecture, false},

	{"сем", kindSeminar, true},
	{"семин", kindSeminar, true},
	{"семинар", kindSeminar, false},
	{"семинары", kindSeminar, false},
	{"семинарское занятие", kindSeminar, false},
	{"семинарские занятия", kindSeminar, false},
	{"пр", kindSeminar, true},
	{"пз", kindSeminar, true},
	{"прак", kindSeminar, true},
	{"практ", kindSeminar, true},
	{"практика", kindSeminar, false},
	{"практическое занятие", kindSeminar, false},
	{"практические занятия", kindSeminar, false},

	{"лаб", kindLab, true},
	{"лр", kindLab, true},
	{"лабораторная", kindLab, false},
	{"лабораторные", kindLab, false},
	{"лабораторная работа", kindLab, false},
	{"лабораторные работы", kindLab, false},
	{"лабораторное занятие", kindLab, false},
	{"лабораторные занятия", kindLab, false},
}

// longestFirst is marks with the longest first, so «лабораторная работа» wins over «лабораторная».
var longestFirst = func() []mark {
	out := slices.Clone(marks)
	slices.SortStableFunc(out, func(a, b mark) int {
		return cmp.Compare(utf8.RuneCountInString(b.text), utf8.RuneCountInString(a.text))
	})
	return out
}()

// separators may stand between a subject and its mark.
func isSeparator(r rune) bool {
	switch r {
	case ',', '-', '–', '—', ':', '/':
		return true
	}
	return false
}

// cutMarks returns text (cleaned) without its lesson-kind marks at the start and the end, and the kind of the last
// mark cut; ok is false when text has none. A text that is only a mark keeps it.
func cutMarks(text string) (base string, k kind, ok bool) {
	d := []rune(values.CleanText(text))
	for len(d) > 0 {
		l := []rune(values.Key(string(d)))
		if len(l) != len(d) {
			break
		}
		start, end, mk, found := locateMark(l)
		if !found {
			break
		}
		next := trimSeparators(d[start:end])
		if len(next) == 0 {
			break
		}
		d, k, ok = next, mk, true
	}
	return string(d), k, ok
}

// locateMark finds one mark at the end of l, else at its start, and returns the part of l that stays.
func locateMark(l []rune) (start, end int, k kind, ok bool) {
	if end, k, ok := markAtEnd(l); ok {
		return 0, end, k, true
	}
	if start, k, ok := markAtStart(l); ok {
		return start, len(l), k, true
	}
	return 0, 0, 0, false
}

func markAtEnd(l []rune) (end int, k kind, ok bool) {
	n := len(l)
	if c := l[n-1]; c == ')' || c == ']' {
		open := lastIndex(l, opener(c))
		if open < 0 {
			return 0, 0, false
		}
		m, found := lookupMark(l[open+1 : n-1])
		return open, m.kind, found
	}

	e, dotted := n, false
	if l[e-1] == '.' {
		e, dotted = e-1, true
	}
	for _, m := range longestFirst {
		mr := []rune(m.text)
		b := e - len(mr)
		if b <= 0 || !slices.Equal(l[b:e], mr) {
			continue
		}
		p := b
		for p > 0 && l[p-1] == ' ' {
			p--
		}
		switch {
		case p > 0 && isSeparator(l[p-1]):
			return p - 1, m.kind, true
		case p < b && m.short && (dotted || len(mr) >= 3):
			return p, m.kind, true
		}
	}
	return 0, 0, false
}

func markAtStart(l []rune) (start int, k kind, ok bool) {
	n := len(l)
	if c := l[0]; c == '(' || c == '[' {
		end := slices.Index(l, closer(c))
		if end < 0 {
			return 0, 0, false
		}
		m, found := lookupMark(l[1:end])
		return end + 1, m.kind, found
	}

	for _, m := range longestFirst {
		mr := []rune(m.text)
		if len(mr) >= n || !slices.Equal(l[:len(mr)], mr) {
			continue
		}
		a, dotted := len(mr), false
		if l[a] == '.' {
			a, dotted = a+1, true
		}
		q := a
		for q < n && l[q] == ' ' {
			q++
		}
		switch {
		case q >= n:
			continue
		case isSeparator(l[q]):
			return q + 1, m.kind, true
		case q > a && dotted && m.short:
			return q, m.kind, true
		}
	}
	return 0, 0, false
}

// lookupMark finds the mark that is the whole of s (spaces and a final dot aside), as inside brackets.
func lookupMark(s []rune) (mark, bool) {
	s = trimRunes(s, func(r rune) bool { return r == ' ' })
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = trimRunes(s[:len(s)-1], func(r rune) bool { return r == ' ' })
	}
	for _, m := range marks {
		if m.text == string(s) {
			return m, true
		}
	}
	return mark{}, false
}

func trimSeparators(s []rune) []rune {
	return trimRunes(s, func(r rune) bool { return r == ' ' || isSeparator(r) })
}

func trimRunes(s []rune, cut func(rune) bool) []rune {
	for len(s) > 0 && cut(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && cut(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

func lastIndex(s []rune, r rune) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == r {
			return i
		}
	}
	return -1
}

func opener(c rune) rune {
	if c == ']' {
		return '['
	}
	return '('
}

func closer(c rune) rune {
	if c == '[' {
		return ']'
	}
	return ')'
}
