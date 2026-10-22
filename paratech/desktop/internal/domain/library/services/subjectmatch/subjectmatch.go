// Package subjectmatch resolves a free subject string (of the schedule, a recording snapshot or the user's
// correction) to a subject of the library, and picks the strings to suggest as new subjects (ADR-0024).
//
// A string matches a subject when its key (values.Key: case-insensitive, «ё» = «е», spaces trimmed and collapsed)
// is the key of the subject's name or of one of its aliases. A key of two subjects is ambiguous and matches none.
// Stored strings are never rewritten: they are resolved each time they are read.
package subjectmatch

import (
	"cmp"
	"slices"

	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Entry is what the index needs of one subject.
type Entry struct {
	ID      shared.SubjectID
	Name    string
	Aliases []string
}

// Match is the subject a string resolves to, with its name as the library shows it.
type Match struct {
	ID   shared.SubjectID
	Name string
}

// Index maps the keys of the names and aliases of the subjects to their ids. The zero value matches nothing.
type Index struct {
	owners map[string][]shared.SubjectID
	names  map[shared.SubjectID]string
}

// NewIndex builds the index of the entries.
func NewIndex(entries []Entry) Index {
	ix := Index{owners: map[string][]shared.SubjectID{}, names: map[shared.SubjectID]string{}}
	for _, e := range entries {
		ix.names[e.ID] = values.CleanText(e.Name)
		for _, s := range append([]string{e.Name}, e.Aliases...) {
			k := values.Key(s)
			if k != "" && !slices.Contains(ix.owners[k], e.ID) {
				ix.owners[k] = append(ix.owners[k], e.ID)
			}
		}
	}
	return ix
}

// Resolve returns the subject s names; ok is false for an empty string, a string of no subject or of two.
func (ix Index) Resolve(s string) (Match, bool) {
	owners := ix.owners[values.Key(s)]
	if len(owners) != 1 {
		return Match{}, false
	}
	return Match{ID: owners[0], Name: ix.names[owners[0]]}, true
}

// TakenByOther reports whether s has the key of a name or alias of a subject other than except: a subject may not
// take a spelling another subject has, so a string never becomes ambiguous through the library itself.
func (ix Index) TakenByOther(s string, except shared.SubjectID) bool {
	for _, id := range ix.owners[values.Key(s)] {
		if id != except {
			return true
		}
	}
	return false
}

// Candidate is a free subject string as stored, with how many live schedule lessons and recordings name it.
type Candidate struct {
	Name       string
	Lessons    int
	Recordings int
}

// Suggestion is a subject the student may create: a free subject string that matches no subject. Spellings with one
// key are merged; Name is the spelling the most lessons and recordings use.
type Suggestion struct {
	Name       string
	Lessons    int
	Recordings int
}

// Suggest returns the candidates that match no subject (also not ambiguously), merged by key, the most used first
// and then by name. It never creates anything: the student decides.
func (ix Index) Suggest(candidates []Candidate) []Suggestion {
	type group struct {
		Suggestion
		best int // the uses of the spelling in Name
	}
	groups := map[string]*group{}
	for _, c := range candidates {
		key := values.Key(c.Name)
		if key == "" || len(ix.owners[key]) > 0 {
			continue
		}
		name, uses := values.CleanText(c.Name), c.Lessons+c.Recordings
		g, ok := groups[key]
		if !ok {
			g = &group{Suggestion: Suggestion{Name: name}, best: uses}
			groups[key] = g
		} else if uses > g.best || (uses == g.best && name < g.Name) {
			g.Name, g.best = name, uses
		}
		g.Lessons += c.Lessons
		g.Recordings += c.Recordings
	}

	out := make([]Suggestion, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.Suggestion)
	}
	slices.SortFunc(out, func(a, b Suggestion) int {
		if c := cmp.Compare(b.Lessons+b.Recordings, a.Lessons+a.Recordings); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}
