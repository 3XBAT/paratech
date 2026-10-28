// Package placement proposes where a file of «Входящие» may go, from its name only (TASK-339): a subject whose name or
// alias the file name contains as whole words, and a system folder from a lesson-kind marker («ЛР», «сем», «лекция»,
// «билеты», «ДЗ»). No AI, and nothing is placed by it: the student confirms the hint or ignores it.
//
// The file name is untrusted text: it is only compared, never copied into the hint. A subject to create is named and
// aliased as the schedule-subjects card would create it (ADR-0033): the caller passes those proposals in.
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

// Proposed is a subject the schedule-subjects card proposes to create (a schedulesubjects.Group of no subject): Name
// without the lesson-kind mark, Spellings the schedule strings it groups (Group.Aliases). Both are matched.
type Proposed struct {
	Name      string
	Spellings []string
}

// Hint is the proposal. KindExisting: SubjectID and FolderID (a system folder; «Материалы» when there is no marker or
// the subject lacks the folder), FolderKind its code. KindCreate: CreateName and CreateAliases of the proposed subject
// (the spellings other than the name, each a valid name, at most subject.MaxAliases) and FolderKind (the folder does
// not exist yet). KindNone: everything zero.
type Hint struct {
	Kind          Kind
	SubjectID     shared.SubjectID
	FolderID      shared.FolderID
	FolderKind    subject.System
	CreateName    string
	CreateAliases []string
}

// Bounds of the work on an untrusted name: a material name has at most material.MaxNameRunes runes, so these only
// matter for a caller that passes something longer.
const (
	maxNameRunes = 1024
	maxTokens    = 256
	maxExtRunes  = 10
	minWeight    = 2 // a single letter («C» of «C++») matches too much
)

// For proposes where a file named fileName goes. subjects are the subjects of the library; schedule the subjects the
// schedule-subjects card proposes to create.
//
// A longer match wins over a shorter one it overlaps («Физика твёрдого тела» over «Физика»), and then the shorter one
// counts nowhere in the name («Физика лек Физика твёрдого тела»). Two subjects matching equally long, or at different
// places of the name («Физика и химия»), give no hint: never guess. A subject match, even an ambiguous one, is never
// replaced by a schedule proposal; the proposals are chosen by the same rule.
func For(fileName string, subjects []Candidate, schedule []Proposed) Hint {
	file := tokens(stem(fileName))
	if len(file) == 0 {
		return Hint{}
	}

	var hits []hit
	for i, c := range subjects {
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			hits = appendHits(hits, file, tokens(n), string(c.ID), i)
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

	for i, p := range schedule {
		name := values.CleanText(p.Name)
		if _, err := subject.NewName(name); err != nil {
			continue // the page could not create it
		}
		key := strings.Join(tokens(name), " ")
		for _, s := range append([]string{name}, p.Spellings...) {
			hits = appendHits(hits, file, trimMarkers(tokens(s)), key, i)
		}
	}
	best, ok := pick(hits)
	if !ok {
		return Hint{}
	}
	p := schedule[best.index]
	name := values.CleanText(p.Name)
	return Hint{Kind: KindCreate, FolderKind: folderKind(file, best.at), CreateName: name, CreateAliases: aliasesOf(name, p.Spellings)}
}

// aliasesOf is the spellings that make valid aliases of a subject named name: one per key, not the key of the name,
// at most subject.MaxAliases.
func aliasesOf(name string, spellings []string) []string {
	var out []string
	seen := map[string]bool{values.Key(name): true}
	for _, s := range spellings {
		s = values.CleanText(s)
		k := values.Key(s)
		if len(out) == subject.MaxAliases {
			break
		}
		if _, err := subject.NewName(s); err != nil || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// span is where a match lies among the tokens of the file name: [from, to).
type span struct{ from, to int }

// hit is a match of one subject (key: its id) or one proposed subject (key: the words of its name); index is its place
// in the input, so the first of several hits with one key is kept.
type hit struct {
	key    string
	index  int
	weight int
	at     span
}

// pick returns the heaviest hit (the first of them). A key with a lighter hit inside a hit of the best key is a part
// of it and counts nowhere; ok is false when any other key has a hit (as heavy, or apart from the best key).
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

	// cover[i] is the weight of the heaviest hit of the best key over token i (file has at most maxTokens tokens).
	var cover [maxTokens]int
	for _, h := range hits {
		if h.key == best.key {
			for i := h.at.from; i < h.at.to; i++ {
				cover[i] = max(cover[i], h.weight)
			}
		}
	}
	part := map[string]bool{}
	for _, h := range hits {
		if h.key == best.key || part[h.key] {
			continue
		}
		for i := h.at.from; i < h.at.to; i++ {
			if cover[i] > h.weight {
				part[h.key] = true
				break
			}
		}
	}
	for _, h := range hits {
		if h.key != best.key && !part[h.key] {
			return hit{}, false
		}
	}
	return best, true
}

// appendHits adds a hit of key for every place words lie in file as a whole sequence of tokens; the weight is the
// runes of the words. file has at most maxTokens tokens, so a name adds at most that many hits.
func appendHits(hits []hit, file, words []string, key string, index int) []hit {
	if len(words) == 0 || len(words) > len(file) {
		return hits
	}
	weight := 0
	for _, w := range words {
		weight += utf8.RuneCountInString(w)
	}
	if weight < minWeight {
		return hits
	}
outer:
	for i := 0; i+len(words) <= len(file); i++ {
		for j, w := range words {
			if file[i+j] != w {
				continue outer
			}
		}
		hits = append(hits, hit{key: key, index: index, weight: weight, at: span{i, i + len(words)}})
	}
	return hits
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
