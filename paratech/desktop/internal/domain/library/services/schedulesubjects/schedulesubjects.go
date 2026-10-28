// Package schedulesubjects proposes the subjects of the library to create from the free subject strings of the
// schedule and the recordings, and turns the student's choices into what to create (ADR-0033 §4, §7; ADR-0024).
//
// Strings with one key once the lesson-kind mark is cut («Матанализ (лек.)», «матанализ, сем.») are an obvious group.
// Abbreviations and typos (relations.go) only link doubtfully: a doubtful member is merged by default (О-12 б) and
// the student may take it separately or skip it. A group linked to one existing subject adds its spellings there.
// Schedule strings are untrusted data (ADR-0022): this package never puts them into an error.
package schedulesubjects

import (
	"cmp"
	"slices"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Kinds counts live lessons by kind.
type Kinds struct {
	Lecture int
	Seminar int // seminars and practices
	Lab     int
	Unknown int
}

// Total is the number of lessons of every kind.
func (k Kinds) Total() int { return k.Lecture + k.Seminar + k.Lab + k.Unknown }

func (k Kinds) plus(o Kinds) Kinds {
	return Kinds{k.Lecture + o.Lecture, k.Seminar + o.Seminar, k.Lab + o.Lab, k.Unknown + o.Unknown}
}

// Row is a free subject string as stored (of the schedule or of a recording), with its live lessons by kind and the
// recordings that name it. Rows with one key add up.
type Row struct {
	Name       string
	Lessons    Kinds
	Recordings int
}

// Link is why a member belongs to its group.
type Link int

// Links. The zero value is LinkSame.
const (
	LinkSame         Link = iota // the same key once the lesson-kind mark is cut
	LinkAbbreviation             // doubtful: one is an abbreviation of the other
	LinkTypo                     // doubtful: they differ by a typo
)

// Member is an obvious group inside a proposed group: the spellings with one key once the mark is cut.
type Member struct {
	Key        string   // the key without the mark; identifies the member in a MemberChoice
	Name       string   // the most used spelling without its mark
	Spellings  []string // cleaned, the most used first
	Kinds      Kinds
	Lessons    int
	Recordings int
	Link       Link
	Doubtful   bool // Link is not LinkSame: the student may take it separately or skip it
}

// Group is a proposed subject, or spellings to add to an existing one when Subject is set.
type Group struct {
	Key        string           // identifies the group in a Choice: the key of its first member
	Name       string           // the name to create; the subject's name when Subject is set
	Subject    shared.SubjectID // set: by default the spellings become aliases of this subject
	Aliases    []string         // the aliases the default choice gives
	Members    []Member         // the lead first, then the more used
	Kinds      Kinds
	Lessons    int
	Recordings int
	Doubtful   bool // a member is doubtful
}

// Proposal is the groups, the most lessons and recordings first, then by name.
type Proposal struct {
	Groups []Group

	index subjectmatch.Index // of the subjects the proposal was made for, to check the new names
}

// spelling is one key of the rows.
type spelling struct {
	text, key  string
	base       string // text without the marks
	best       int    // the uses of the row text was taken from
	kinds      Kinds
	recordings int
}

func (s *spelling) uses() int { return s.kinds.Total() + s.recordings }

// cluster is an obvious group while proposing.
type cluster struct {
	Member
	members []*cluster // linked doubtfully to this lead
}

func (c *cluster) uses() int { return c.Lessons + c.Recordings }

// Propose groups the rows that match no subject (also not ambiguously) and links them to the subjects.
//
// Doubtful links never chain: a cluster joins a subject only when it is linked to exactly one; the others join a
// lead in two rounds, typos first (the more used is the lead), then abbreviations (the longer is the lead), each only
// when exactly one lead fits and only to a lead, never to a member of another lead.
func Propose(rows []Row, subjects []subjectmatch.Entry) Proposal {
	ix := subjectmatch.NewIndex(subjects)
	clusters := clustersOf(rows, ix)
	known := subjectKeysOf(subjects)

	bySubject := map[shared.SubjectID][]*cluster{}
	var free []*cluster
	for _, c := range clusters {
		id, link, drop := attach(c.Key, known)
		switch {
		case drop:
		case id == "":
			free = append(free, c)
		default:
			c.Link, c.Doubtful = link, link != LinkSame
			bySubject[id] = append(bySubject[id], c)
		}
	}

	p := Proposal{index: ix}
	for _, lead := range leads(free) {
		p.Groups = append(p.Groups, newGroup(lead.Name, "", append([]*cluster{lead}, lead.members...)))
	}
	for _, k := range known {
		if cs := bySubject[k.id]; len(cs) > 0 {
			p.Groups = append(p.Groups, newGroup(k.name, k.id, cs))
		}
	}
	slices.SortFunc(p.Groups, func(a, b Group) int {
		return byUse(a.Lessons+a.Recordings, b.Lessons+b.Recordings, a.Name, b.Name, a.Key, b.Key)
	})
	return p
}

// clustersOf adds the rows up by key and groups the keys by key without the mark, ordered by key.
func clustersOf(rows []Row, ix subjectmatch.Index) []*cluster {
	spellings := map[string]*spelling{}
	for _, r := range rows {
		text := values.CleanText(r.Name)
		if ix.Lookup(text).Outcome != subjectmatch.NoMatch {
			continue
		}
		if _, err := subject.NewName(text); err != nil {
			continue
		}
		base, k, marked := cutMarks(text)
		kinds := Kinds{max(r.Lessons.Lecture, 0), max(r.Lessons.Seminar, 0), max(r.Lessons.Lab, 0), max(r.Lessons.Unknown, 0)}
		if marked {
			kinds = kinds.known(k)
		}
		recs, uses := max(r.Recordings, 0), kinds.Total()+max(r.Recordings, 0)

		key := values.Key(text)
		s, ok := spellings[key]
		if !ok {
			s = &spelling{text: text, key: key, base: base, best: uses}
			spellings[key] = s
		} else if uses > s.best || (uses == s.best && text < s.text) {
			s.text, s.base, s.best = text, base, uses
		}
		s.kinds = s.kinds.plus(kinds)
		s.recordings += recs
	}

	byBase := map[string][]*spelling{}
	for _, s := range spellings {
		k := values.Key(s.base)
		byBase[k] = append(byBase[k], s)
	}
	out := make([]*cluster, 0, len(byBase))
	for key, ss := range byBase {
		slices.SortFunc(ss, func(a, b *spelling) int { return byUse(a.uses(), b.uses(), a.text, b.text, a.key, b.key) })
		c := &cluster{Member: Member{Key: key, Name: ss[0].base}}
		for _, s := range ss {
			c.Spellings = append(c.Spellings, s.text)
			c.Kinds = c.Kinds.plus(s.kinds)
			c.Recordings += s.recordings
		}
		c.Lessons = c.Kinds.Total()
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b *cluster) int { return cmp.Compare(a.Key, b.Key) })
	return out
}

// known counts the lessons of unknown kind as of kind k: the mark says what the schedule did not.
func (k Kinds) known(as kind) Kinds {
	switch as {
	case kindLecture:
		k.Lecture += k.Unknown
	case kindSeminar:
		k.Seminar += k.Unknown
	case kindLab:
		k.Lab += k.Unknown
	default:
		return k
	}
	k.Unknown = 0
	return k
}

// subjectKeys is an existing subject with the keys of its name and aliases without their marks.
type subjectKeys struct {
	id   shared.SubjectID
	name string
	keys []string
}

// subjectKeysOf is the subjects by id; a repeated id keeps its last entry, as subjectmatch.NewIndex.
func subjectKeysOf(entries []subjectmatch.Entry) []subjectKeys {
	byID := map[shared.SubjectID]subjectKeys{}
	for _, e := range entries {
		k := subjectKeys{id: e.ID, name: values.CleanText(e.Name)}
		for _, s := range append([]string{e.Name}, e.Aliases...) {
			base, _, _ := cutMarks(s)
			if key := values.Key(base); key != "" && !slices.Contains(k.keys, key) {
				k.keys = append(k.keys, key)
			}
		}
		byID[e.ID] = k
	}
	out := make([]subjectKeys, 0, len(byID))
	for _, k := range byID {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b subjectKeys) int { return cmp.Compare(a.id, b.id) })
	return out
}

// attach finds the subject key belongs to: the one subject with the same key (drop when several have it), else the
// one subject it is doubtfully linked to (none when several are).
func attach(key string, known []subjectKeys) (id shared.SubjectID, link Link, drop bool) {
	var same, near []shared.SubjectID
	var nearLink Link
	for _, k := range known {
		best, found := Link(0), false
		for _, sk := range k.keys {
			if l, ok := related(key, sk); ok && (!found || l < best) {
				best, found = l, true
			}
		}
		switch {
		case !found:
		case best == LinkSame:
			same = append(same, k.id)
		default:
			near, nearLink = append(near, k.id), best
		}
	}
	switch {
	case len(same) > 1:
		return "", 0, true
	case len(same) == 1:
		return same[0], LinkSame, false
	case len(near) == 1:
		return near[0], nearLink, false
	}
	return "", 0, false
}

// leads joins the free clusters to their leads (see Propose) and returns the leads.
func leads(free []*cluster) []*cluster {
	slices.SortFunc(free, func(a, b *cluster) int { return byUse(a.uses(), b.uses(), a.Name, b.Name, a.Key, b.Key) })
	typoLeads := join(free, func(c, lead *cluster) (Link, bool) { return LinkTypo, isTypo(c.Key, lead.Key) })

	slices.SortFunc(typoLeads, func(a, b *cluster) int {
		if c := cmp.Compare(len(compact(b.Key)), len(compact(a.Key))); c != 0 {
			return c
		}
		return byUse(a.uses(), b.uses(), a.Name, b.Name, a.Key, b.Key)
	})
	return join(typoLeads, func(c, lead *cluster) (Link, bool) {
		return LinkAbbreviation, isShortForm(c.Key, lead.Key)
	})
}

// join walks ordered: a cluster linked to exactly one earlier lead becomes its member with its own members, else it
// is a lead.
func join(ordered []*cluster, linked func(c, lead *cluster) (Link, bool)) []*cluster {
	var out []*cluster
	for _, c := range ordered {
		var to *cluster
		var link Link
		n := 0
		for _, lead := range out {
			if l, ok := linked(c, lead); ok {
				to, link, n = lead, l, n+1
			}
		}
		if n != 1 {
			out = append(out, c)
			continue
		}
		c.Link, c.Doubtful = link, true
		to.members = append(to.members, c)
		to.members = append(to.members, c.members...)
		c.members = nil
	}
	return out
}

// newGroup makes the group of the clusters; a new subject (id "") keeps its lead first.
func newGroup(name string, id shared.SubjectID, cs []*cluster) Group {
	rest := cs
	if id == "" {
		rest = cs[1:]
	}
	slices.SortFunc(rest, func(a, b *cluster) int {
		if a.Doubtful != b.Doubtful {
			if !a.Doubtful {
				return -1
			}
			return 1
		}
		return byUse(a.uses(), b.uses(), a.Name, b.Name, a.Key, b.Key)
	})

	g := Group{Name: name, Subject: id}
	for _, c := range cs {
		g.Members = append(g.Members, c.Member)
		g.Kinds = g.Kinds.plus(c.Kinds)
		g.Recordings += c.Recordings
		g.Doubtful = g.Doubtful || c.Doubtful
	}
	g.Key, g.Lessons = g.Members[0].Key, g.Kinds.Total()
	nameKey := ""
	if id == "" {
		nameKey = values.Key(name)
	}
	g.Aliases = aliasesOf(g.Members, nameKey)
	return g
}

// aliasesOf is the spellings of the members, one per key, without the key of the name.
func aliasesOf(members []Member, nameKey string) []string {
	var out []string
	seen := map[string]bool{nameKey: true}
	for _, m := range members {
		for _, s := range m.Spellings {
			if k := values.Key(s); !seen[k] {
				seen[k] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// byUse orders the most used first, then by name and key.
func byUse(aUses, bUses int, aName, bName, aKey, bKey string) int {
	if c := cmp.Compare(bUses, aUses); c != 0 {
		return c
	}
	if c := cmp.Compare(aName, bName); c != 0 {
		return c
	}
	return cmp.Compare(aKey, bKey)
}
