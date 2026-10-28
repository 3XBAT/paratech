// Package placement proposes where a file of «Входящие» may go, from its name only (TASK-339): a subject whose name or
// alias the file name contains as whole words, and a system folder from a lesson-kind marker («ЛР», «сем», «лекция»,
// «билеты», «ДЗ»). No AI, and nothing is placed by it: the student confirms the hint or ignores it.
//
// The file name is untrusted text: it is only compared, never copied into the hint. A subject to create is named as
// the schedule spells it.
package placement

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Kind is what the hint proposes.
type Kind int

// Kinds of a hint. The zero value is KindNone.
const (
	KindNone     Kind = iota // nothing to propose (no match, or an ambiguous one)
	KindExisting             // put it into a subject of the library
	KindCreate               // create the subject the schedule names and put it there
)

// Folder is a folder of a candidate subject; System is "" for an own folder.
type Folder struct {
	ID     shared.FolderID
	System subject.System
}

// Candidate is a subject of the library as the hint needs it.
type Candidate struct {
	ID      shared.SubjectID
	Name    string
	Aliases []string
	Folders []Folder
}

// Hint is the proposal. KindExisting: SubjectID and FolderID (a system folder; «Материалы» when there is no marker or
// the subject lacks the folder), FolderKind its code. KindCreate: CreateName as the schedule spells it and FolderKind
// (the folder does not exist yet). KindNone: everything zero.
type Hint struct {
	Kind       Kind
	SubjectID  shared.SubjectID
	FolderID   shared.FolderID
	FolderKind subject.System
	CreateName string
}

// Bounds of the work on an untrusted name: a material name has at most material.MaxNameRunes runes, so these only
// matter for a caller that passes something longer.
const (
	maxNameRunes = 1024
	maxTokens    = 256
	maxExtRunes  = 10
	minWeight    = 2 // a single letter («C» of «C++») matches too much
)

// For proposes where a file named fileName goes. subjects are the subjects of the library; schedule the subject
// strings of the schedule and the recordings that match no subject (SuggestSubjects).
//
// A longer match wins over a shorter one it overlaps («Физика твёрдого тела» over «Физика»). Two subjects matching
// equally long, or at different places of the name («Физика и химия»), give no hint: never guess. A subject match,
// even an ambiguous one, is never replaced by a schedule string; the schedule strings are chosen by the same rule.
func For(fileName string, subjects []Candidate, schedule []string) Hint {
	file := tokens(stem(fileName))
	if len(file) == 0 {
		return Hint{}
	}

	var hits []hit
	for i, c := range subjects {
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			if w, at, ok := find(file, tokens(n)); ok {
				hits = append(hits, hit{key: string(c.ID), index: i, weight: w, at: at})
			}
		}
	}
	if len(hits) > 0 {
		best, ok := pick(hits)
		if !ok {
			return Hint{}
		}
		c := subjects[best.index]
		kind := folderKind(file, best.at)
		f, ok := systemFolder(c.Folders, kind)
		if !ok {
			kind = subject.SystemMaterials
			f, _ = systemFolder(c.Folders, kind) // none: "" is «Материалы» for Place
		}
		return Hint{Kind: KindExisting, SubjectID: c.ID, FolderID: f, FolderKind: kind}
	}

	for i, s := range schedule {
		if _, err := subject.NewName(values.CleanText(s)); err != nil {
			continue // the page could not create it
		}
		words := trimMarkers(tokens(s))
		if w, at, ok := find(file, words); ok {
			hits = append(hits, hit{key: strings.Join(words, " "), index: i, weight: w, at: at})
		}
	}
	best, ok := pick(hits)
	if !ok {
		return Hint{}
	}
	return Hint{Kind: KindCreate, FolderKind: folderKind(file, best.at), CreateName: values.CleanText(schedule[best.index])}
}

// span is where a match lies among the tokens of the file name: [from, to).
type span struct{ from, to int }

func (s span) overlaps(o span) bool { return s.from < o.to && o.from < s.to }

// hit is a match of one subject (key: its id) or one schedule string (key: its words); index is its place in the
// input, so the first of several hits with one key is kept.
type hit struct {
	key    string
	index  int
	weight int
	at     span
}

// pick returns the heaviest hit; ok is false when a hit of another key is as heavy or lies apart from it.
func pick(hits []hit) (hit, bool) {
	if len(hits) == 0 {
		return hit{}, false
	}
	best := hits[0]
	for _, h := range hits[1:] {
		if h.weight > best.weight {
			best = h
		}
	}
	for _, h := range hits {
		if h.key != best.key && (h.weight == best.weight || !h.at.overlaps(best.at)) {
			return hit{}, false
		}
	}
	return best, true
}

// find looks for words as a whole sequence of tokens of file; the weight is the runes of the words.
func find(file, words []string) (weight int, at span, ok bool) {
	if len(words) == 0 || len(words) > len(file) {
		return 0, span{}, false
	}
	for _, w := range words {
		weight += utf8.RuneCountInString(w)
	}
	if weight < minWeight {
		return 0, span{}, false
	}
outer:
	for i := 0; i+len(words) <= len(file); i++ {
		for j, w := range words {
			if file[i+j] != w {
				continue outer
			}
		}
		return weight, span{i, i + len(words)}, true
	}
	return 0, span{}, false
}

// stem is the base name of a file without its extension.
func stem(name string) string {
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = string([]rune(name)[:maxNameRunes])
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndexByte(name, '.'); i > 0 && extension(name[i+1:]) {
		name = name[:i]
	}
	return name
}

// extension reports whether s looks like a file extension: a few letters or digits.
func extension(s string) bool {
	n := utf8.RuneCountInString(s)
	if n == 0 || n > maxExtRunes {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// tokens splits the key of s (values.Key: lower case, «ё» = «е») into runs of letters and runs of digits; anything
// else («_», «-», «.», brackets, spaces) is a boundary, and so is a change between letters and digits («лр2»).
func tokens(s string) []string {
	var out []string
	var cur []rune
	digits := false
	flush := func() {
		if len(cur) > 0 && len(out) < maxTokens {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range values.Key(s) {
		isDigit := unicode.IsDigit(r)
		// A combining mark belongs to its letter, so a decomposed «й» does not split a word.
		isWord := isDigit || unicode.IsLetter(r) || (unicode.Is(unicode.Mn, r) && len(cur) > 0 && !digits)
		switch {
		case !isWord:
			flush()
			continue
		case len(cur) > 0 && isDigit != digits:
			flush()
		}
		cur, digits = append(cur, r), isDigit
	}
	flush()
	return out
}
