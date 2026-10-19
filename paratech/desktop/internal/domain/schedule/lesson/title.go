package lesson

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// titleSeparators split a title into a subject and a kind word: "Матанализ, лекция", "Лекция: Матанализ",
// "Физика - практика". Dashes count only with spaces around them, so "Пр-ка" or "Pre-calculus" stay whole.
var titleSeparators = []string{":", ",", " - ", " – ", " — "}

// topicWords join a kind and a subject: "Лекция по физике", "Lecture on calculus".
var topicWords = []string{"по", "on"}

// ClassifyTitle derives the subject and the kind of a class from its title, for sources without explicit columns
// (ICS, CSV and XLSX rows without them). The rule is conservative: a kind is recognized only as a whole kind word or
// abbreviation (ParseKind) in one of these places, never inside a word ("Лабиринты истории" is not a lab):
//
//   - in brackets at the end or the start: "Матанализ (лек.)", "Calculus [lab]", "(сем) Физика";
//   - after the last or before the first separator (comma, colon, a dash with spaces): "Матанализ, лекция",
//     "Лекция: Матанализ", "Физика - практика";
//   - before "по" or "on": "Лекция по физике" gives the subject "Физике". Only the first letter is capitalized; the
//     words keep their grammatical case (no declension);
//   - the whole title: "Лекция" or "Практикум" is that kind with the title as the subject.
//
// The subject is the rest of the title, trimmed of separators. Otherwise the kind is KindUnknown and the subject is
// the title. The subject is a clean line of at most MaxSubjectRunes (CleanText); a blank title gives "".
func ClassifyTitle(title string) (subject string, kind Kind) {
	t := CleanText(title, utf8.RuneCountInString(title))
	whole := CleanText(t, MaxSubjectRunes)
	if t == "" {
		return "", KindUnknown
	}
	if k, ok := knownKind(t); ok {
		return whole, k
	}

	for _, rule := range []func(string) (string, Kind, bool){bracketAtEnd, bracketAtStart, kindAfterSeparator, kindBeforeSeparator, kindWithTopic} {
		s, k, ok := rule(t)
		if !ok {
			continue
		}
		if s = CleanText(strings.Trim(s, " ,;:-–—"), MaxSubjectRunes); s != "" {
			return s, k
		}
	}
	return whole, KindUnknown
}

// knownKind is ParseKind without the words of KindUnknown: a title saying "Другое" says nothing about the kind.
func knownKind(s string) (Kind, bool) {
	k, ok := ParseKind(s)
	return k, ok && k != KindUnknown
}

var closingBracket = map[byte]byte{'(': ')', '[': ']'}

// bracketAtEnd: "Матанализ (лек.)".
func bracketAtEnd(t string) (string, Kind, bool) {
	for open, closing := range closingBracket {
		if !strings.HasSuffix(t, string(closing)) {
			continue
		}
		i := strings.LastIndexByte(t, open)
		if i < 0 {
			continue
		}
		if k, ok := knownKind(t[i+1 : len(t)-1]); ok {
			return t[:i], k, true
		}
	}
	return "", KindUnknown, false
}

// bracketAtStart: "(сем) Физика".
func bracketAtStart(t string) (string, Kind, bool) {
	for open, closing := range closingBracket {
		if t[0] != open {
			continue
		}
		i := strings.IndexByte(t, closing)
		if i < 0 {
			continue
		}
		if k, ok := knownKind(t[1:i]); ok {
			return t[i+1:], k, true
		}
	}
	return "", KindUnknown, false
}

// kindAfterSeparator: "Матанализ, лекция".
func kindAfterSeparator(t string) (string, Kind, bool) {
	for _, sep := range titleSeparators {
		i := strings.LastIndex(t, sep)
		if i < 0 {
			continue
		}
		if k, ok := knownKind(t[i+len(sep):]); ok {
			return t[:i], k, true
		}
	}
	return "", KindUnknown, false
}

// kindBeforeSeparator: "Лекция: Матанализ".
func kindBeforeSeparator(t string) (string, Kind, bool) {
	for _, sep := range titleSeparators {
		i := strings.Index(t, sep)
		if i < 0 {
			continue
		}
		if k, ok := knownKind(t[:i]); ok {
			return t[i+len(sep):], k, true
		}
	}
	return "", KindUnknown, false
}

// kindWithTopic: "Лекция по физике", "Lab work on optics". The kind is everything before the first "по"/"on".
func kindWithTopic(t string) (string, Kind, bool) {
	words := strings.Fields(t)
	for i := 1; i < len(words)-1; i++ {
		if !isTopicWord(words[i]) {
			continue
		}
		k, ok := knownKind(strings.Join(words[:i], " "))
		if !ok {
			return "", KindUnknown, false
		}
		return capitalize(strings.Join(words[i+1:], " ")), k, true
	}
	return "", KindUnknown, false
}

func isTopicWord(w string) bool {
	for _, t := range topicWords {
		if strings.EqualFold(w, t) {
			return true
		}
	}
	return false
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
