package schedulesubjects

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

// Errors of Apply (ARCH-18). They never hold a schedule string (ADR-0022).
var (
	// ErrUnknownGroup means a choice names a group the proposal does not have.
	ErrUnknownGroup = errors.New("unknown schedule group")

	// ErrUnknownMember means a choice names a member its group does not have.
	ErrUnknownMember = errors.New("unknown schedule group member")

	// ErrInvalidChoice means a choice the group or member does not allow, or a repeated one.
	ErrInvalidChoice = errors.New("invalid schedule subject choice")

	// ErrNameTaken means two subjects of the outcome, or a new one and an existing one, would share a spelling.
	ErrNameTaken = errors.New("schedule subject name is taken")

	// ErrProposalNotBuilt means a proposal with groups that Propose did not make: its new names cannot be checked
	// against the existing subjects.
	ErrProposalNotBuilt = errors.New("schedule proposal not built by Propose")
)

// GroupAction is what to do with a group. The zero value is the group's default: create it, or add its spellings to
// its subject.
type GroupAction int

// Group actions.
const (
	GroupDefault      GroupAction = iota
	GroupCreate                   // a group without a subject only
	GroupAddToSubject             // a group with a subject only
	GroupSkip                     // «Не заводить»: the merged members are left out
)

// MemberAction is what to do with a member. The zero value merges it into its group.
type MemberAction int

// Member actions.
const (
	MemberMerge      MemberAction = iota
	MemberSeparately              // «Отдельно»: a doubtful member only, it becomes a subject of its own
	MemberSkip                    // «Не заводить»
)

// MemberChoice is the student's choice for a member; Name renames a member taken separately.
type MemberChoice struct {
	Member string // Member.Key
	Action MemberAction
	Name   string
}

// Choice is the student's choice for a group; Name renames a group that is created. A group without a choice and a
// member without one take the defaults.
type Choice struct {
	Group   string // Group.Key
	Action  GroupAction
	Name    string
	Members []MemberChoice
}

// Create is a subject to create. Key is the key to derive its id from (DeriveSubjectID). Aliases are at most
// subject.MaxAliases, so subject.New takes them: the first spellings by member (the lead first, then the more used),
// each member's most used first.
type Create struct {
	Key        string
	Name       string
	Aliases    []string
	Kinds      Kinds
	Lessons    int
	Recordings int
}

// Addition is spellings to add as aliases to an existing subject; Name is the subject's name. The free alias slots
// of the subject are unknown here, so the caller handles subject.ErrTooMany of AddAlias.
type Addition struct {
	Subject    shared.SubjectID
	Name       string
	Aliases    []string
	Kinds      Kinds
	Lessons    int
	Recordings int
}

// Outcome is what the choices make of a proposal, each list the most lessons and recordings first, then by name.
type Outcome struct {
	Creates   []Create
	Additions []Addition
}

// Apply turns the proposal and the student's choices into the subjects to create and the aliases to add. The
// proposal must come from Propose run with the current subjects right before Apply: the names are checked against
// the subjects it was made for. Errors: ErrProposalNotBuilt, ErrUnknownGroup, ErrUnknownMember, ErrInvalidChoice,
// ErrNameTaken, subject.ErrInvalidName for a new name.
func Apply(p Proposal, choices []Choice) (Outcome, error) {
	if len(p.Groups) > 0 && !p.built {
		return Outcome{}, ErrProposalNotBuilt
	}

	byGroup := map[string]Choice{}
	for _, c := range choices {
		if _, dup := byGroup[c.Group]; dup {
			return Outcome{}, ErrInvalidChoice
		}
		if !slices.ContainsFunc(p.Groups, func(g Group) bool { return g.Key == c.Group }) {
			return Outcome{}, ErrUnknownGroup
		}
		byGroup[c.Group] = c
	}

	var out Outcome
	for _, g := range p.Groups {
		if err := applyGroup(&out, g, byGroup[g.Key]); err != nil {
			return Outcome{}, err
		}
	}
	if err := checkSpellings(out, p.index); err != nil {
		return Outcome{}, err
	}

	slices.SortFunc(out.Creates, func(a, b Create) int {
		return byUse(a.Lessons+a.Recordings, b.Lessons+b.Recordings, a.Name, b.Name, a.Key, b.Key)
	})
	slices.SortFunc(out.Additions, func(a, b Addition) int {
		return byUse(a.Lessons+a.Recordings, b.Lessons+b.Recordings, a.Name, b.Name, string(a.Subject), string(b.Subject))
	})
	return out, nil
}

func applyGroup(out *Outcome, g Group, c Choice) error {
	action := c.Action
	if action == GroupDefault {
		action = GroupCreate
		if g.Subject != "" {
			action = GroupAddToSubject
		}
	}
	switch {
	case action == GroupCreate && g.Subject != "",
		action == GroupAddToSubject && g.Subject == "",
		action != GroupCreate && c.Name != "",
		action < GroupDefault || action > GroupSkip:
		return ErrInvalidChoice
	}

	byMember := map[string]MemberChoice{}
	for _, mc := range c.Members {
		i := slices.IndexFunc(g.Members, func(m Member) bool { return m.Key == mc.Member })
		if i < 0 {
			return ErrUnknownMember
		}
		_, dup := byMember[mc.Member]
		switch {
		case dup,
			mc.Action < MemberMerge || mc.Action > MemberSkip,
			mc.Action == MemberSeparately && !g.Members[i].Doubtful,
			mc.Action != MemberSeparately && mc.Name != "":
			return ErrInvalidChoice
		}
		byMember[mc.Member] = mc
	}

	var merged []Member
	for _, m := range g.Members {
		switch mc := byMember[m.Key]; mc.Action {
		case MemberMerge:
			merged = append(merged, m)
		case MemberSeparately:
			cr, err := newCreate(m.Key, m.Name, mc.Name, []Member{m})
			if err != nil {
				return err
			}
			out.Creates = append(out.Creates, cr)
		}
	}
	if len(merged) == 0 {
		return nil
	}

	switch action {
	case GroupCreate:
		cr, err := newCreate(g.Key, g.Name, c.Name, merged)
		if err != nil {
			return err
		}
		out.Creates = append(out.Creates, cr)
	case GroupAddToSubject:
		a := Addition{Subject: g.Subject, Name: g.Name, Aliases: aliasesOf(merged, "")}
		a.Kinds, a.Recordings = sum(merged)
		a.Lessons = a.Kinds.Total()
		out.Additions = append(out.Additions, a)
	}
	return nil
}

// newCreate makes the subject of the members, named rename when it is set and else name.
func newCreate(key, name, rename string, members []Member) (Create, error) {
	if rename != "" {
		n, err := subject.NewName(rename)
		if err != nil {
			return Create{}, err
		}
		name = n.String()
	}
	aliases := aliasesOf(members, values.Key(name))
	cr := Create{Key: key, Name: name, Aliases: aliases[:min(len(aliases), subject.MaxAliases)]}
	cr.Kinds, cr.Recordings = sum(members)
	cr.Lessons = cr.Kinds.Total()
	return cr, nil
}

func sum(members []Member) (Kinds, int) {
	var k Kinds
	recs := 0
	for _, m := range members {
		k, recs = k.plus(m.Kinds), recs+m.Recordings
	}
	return k, recs
}

// checkSpellings checks that no two subjects of the outcome share a key and that no new name is a spelling of an
// existing subject: a rename may pick one.
func checkSpellings(out Outcome, ix subjectmatch.Index) error {
	seen := map[string]bool{}
	take := func(s string) error {
		k := values.Key(s)
		if seen[k] {
			return ErrNameTaken
		}
		seen[k] = true
		return nil
	}
	for _, c := range out.Creates {
		if ix.Lookup(c.Name).Outcome != subjectmatch.NoMatch {
			return ErrNameTaken
		}
		for _, s := range append([]string{c.Name}, c.Aliases...) {
			if err := take(s); err != nil {
				return err
			}
		}
	}
	for _, a := range out.Additions {
		for _, s := range a.Aliases {
			if err := take(s); err != nil {
				return err
			}
		}
	}
	return nil
}

// idPrefix marks the ids derived for subjects from the schedule.
const idPrefix = "sched-"

// DeriveSubjectID is the id of the subject created for the group (or member) key of the proposal with token, so a
// retry of the same choices creates nothing twice (hard rule 7). The token and the key are length-prefixed, so two
// pairs never hash alike by where they split.
func DeriveSubjectID(token, key string) shared.SubjectID {
	b := binary.BigEndian.AppendUint64(nil, uint64(len(token)))
	b = append(append(b, token...), key...)
	sum := sha256.Sum256(b)
	return shared.SubjectID(idPrefix + hex.EncodeToString(sum[:16]))
}
