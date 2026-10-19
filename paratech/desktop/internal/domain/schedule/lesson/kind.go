package lesson

import (
	"fmt"
	"strings"
)

// Kind is the type of a class: lecture, seminar, practice or lab. The zero value is KindUnknown ("другое/неизвестно").
type Kind uint8

// Kinds of a lesson.
const (
	KindUnknown Kind = iota
	KindLecture
	KindSeminar
	KindPractice
	KindLab
)

// Kinds lists every kind in a stable order; the CHECK of schedule_entries.kind holds the same codes.
func Kinds() []Kind { return []Kind{KindUnknown, KindLecture, KindSeminar, KindPractice, KindLab} }

var kindCodes = map[Kind]string{
	KindUnknown:  "unknown",
	KindLecture:  "lecture",
	KindSeminar:  "seminar",
	KindPractice: "practice",
	KindLab:      "lab",
}

// String returns the stable code of the kind ("lecture", ..., "unknown"), used in storage and on the wire.
func (k Kind) String() string {
	if code, ok := kindCodes[k]; ok {
		return code
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Validate reports whether k is one of Kinds.
func (k Kind) Validate() error {
	if _, ok := kindCodes[k]; !ok {
		return fmt.Errorf("%w: kind %d", ErrInvalidLesson, uint8(k))
	}
	return nil
}

// KindOf parses a stable code as String returns it; anything else is ErrInvalidLesson.
func KindOf(code string) (Kind, error) {
	for k, c := range kindCodes {
		if c == code {
			return k, nil
		}
	}
	return KindUnknown, fmt.Errorf("%w: kind %q", ErrInvalidLesson, code)
}

// kindWords maps the normalized words of a kind (see normalizeKindWord) to the kind: Russian and English names,
// common abbreviations of university schedules and the stable codes.
var kindWords = map[string]Kind{
	"unknown": KindUnknown, "other": KindUnknown, "другое": KindUnknown, "неизвестно": KindUnknown,

	"lecture": KindLecture, "lec": KindLecture, "lect": KindLecture,
	"лекция": KindLecture, "лек": KindLecture, "лк": KindLecture, "лекционное занятие": KindLecture,

	"seminar": KindSeminar, "sem": KindSeminar,
	"семинар": KindSeminar, "сем": KindSeminar, "семинарское занятие": KindSeminar,

	"practice": KindPractice, "practical": KindPractice, "practical class": KindPractice,
	"практика": KindPractice, "пр": KindPractice, "практ": KindPractice, "пз": KindPractice,
	"практическое занятие": KindPractice, "практическое": KindPractice, "практикум": KindPractice,

	"lab": KindLab, "laboratory": KindLab, "lab work": KindLab, "laboratory work": KindLab,
	"лабораторная": KindLab, "лаб": KindLab, "лр": KindLab, "лабораторная работа": KindLab,
	"лаб работа": KindLab, "лаб раб": KindLab,
}

// normalizeKindWord lowercases the text, turns dots into spaces ("лек." and "лаб.раб." are abbreviations) and
// collapses spaces.
func normalizeKindWord(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.ToLower(s), ".", " ")), " ")
}

// ParseKind reads a kind as a person writes it in a schedule: a Russian or English name or abbreviation, in any
// case, with dots ("лекция", "Лек.", "seminar", "пр", "lab"). It reports false for anything else, the empty text
// included: the caller decides what an empty cell means.
func ParseKind(s string) (Kind, bool) {
	k, ok := kindWords[normalizeKindWord(s)]
	return k, ok
}
