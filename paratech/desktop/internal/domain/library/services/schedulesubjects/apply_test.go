package schedulesubjects

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
)

// fixture is a proposal with a new subject with a doubtful member, a plain one and a group of an existing subject.
func fixture() Proposal {
	return Propose([]Row{
		{Name: "Математический анализ (лек.)", Lessons: lec(4)},
		{Name: "Матанализ, сем.", Lessons: sem(2)},
		{Name: "Матан", Recordings: 1},
		{Name: "Информатика лаб.", Lessons: lab(2)},
		{Name: "Физика - лабораторная", Lessons: lab(2)},
		{Name: "Физики", Lessons: lec(1)},
	}, []subjectmatch.Entry{
		{ID: "s-phys", Name: "Физика"},
		{ID: "s-hist", Name: "История", Aliases: []string{"История России"}},
	})
}

func TestApplyDefaults(t *testing.T) {
	out, err := Apply(fixture(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Outcome{
		Creates: []Create{
			{
				Key: "математический анализ", Name: "Математический анализ",
				Aliases: []string{"Математический анализ (лек.)", "Матанализ, сем.", "Матан"},
				Kinds:   Kinds{Lecture: 4, Seminar: 2}, Lessons: 6, Recordings: 1,
			},
			{Key: "информатика", Name: "Информатика", Aliases: []string{"Информатика лаб."}, Kinds: lab(2), Lessons: 2},
		},
		Additions: []Addition{{
			Subject: "s-phys", Name: "Физика", Aliases: []string{"Физика - лабораторная", "Физики"},
			Kinds: Kinds{Lecture: 1, Lab: 2}, Lessons: 3,
		}},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("outcome\n got %+v\nwant %+v", out, want)
	}
}

func TestApplyChoices(t *testing.T) {
	tests := []struct {
		name      string
		choices   []Choice
		creates   []string // Name: aliases
		additions []string
	}{
		{
			name:    "rename a group: an alias with the new key goes",
			choices: []Choice{{Group: "математический анализ", Name: " матанализ,  сем. "}},
			creates: []string{
				"матанализ, сем.: [Математический анализ (лек.) Матан]",
				"Информатика: [Информатика лаб.]",
			},
			additions: []string{"Физика: [Физика - лабораторная Физики]"},
		},
		{
			name: "a doubtful member separately, renamed",
			choices: []Choice{{Group: "математический анализ", Members: []MemberChoice{
				{Member: "матан", Action: MemberSeparately, Name: "Матан (старый курс)"},
			}}},
			creates: []string{
				"Математический анализ: [Математический анализ (лек.) Матанализ, сем.]",
				"Информатика: [Информатика лаб.]",
				"Матан (старый курс): [Матан]",
			},
			additions: []string{"Физика: [Физика - лабораторная Физики]"},
		},
		{
			name: "a doubtful member skipped",
			choices: []Choice{{Group: "математический анализ", Members: []MemberChoice{
				{Member: "матанализ", Action: MemberSkip},
			}}},
			creates: []string{
				"Математический анализ: [Математический анализ (лек.) Матан]",
				"Информатика: [Информатика лаб.]",
			},
			additions: []string{"Физика: [Физика - лабораторная Физики]"},
		},
		{
			name: "a group skipped keeps its separate member",
			choices: []Choice{{Group: "математический анализ", Action: GroupSkip, Members: []MemberChoice{
				{Member: "матанализ", Action: MemberSeparately},
			}}},
			creates:   []string{"Информатика: [Информатика лаб.]", "Матанализ: [Матанализ, сем.]"}, // 2 lessons each: by name
			additions: []string{"Физика: [Физика - лабораторная Физики]"},
		},
		{
			name: "explicit actions; a doubtful spelling of a subject becomes its own",
			choices: []Choice{
				{Group: "информатика", Action: GroupCreate},
				{Group: "физика", Action: GroupAddToSubject, Members: []MemberChoice{{Member: "физики", Action: MemberSeparately}}},
			},
			creates: []string{
				"Математический анализ: [Математический анализ (лек.) Матанализ, сем. Матан]",
				"Информатика: [Информатика лаб.]",
				"Физики: []",
			},
			additions: []string{"Физика: [Физика - лабораторная]"},
		},
		{
			name: "everything skipped",
			choices: []Choice{
				{Group: "математический анализ", Action: GroupSkip},
				{Group: "информатика", Action: GroupSkip},
				{Group: "физика", Members: []MemberChoice{{Member: "физика"}, {Member: "физики", Action: MemberSkip}}},
			},
			additions: []string{"Физика: [Физика - лабораторная]"},
		},
		{
			name: "all members of a group skipped",
			choices: []Choice{
				{Group: "физика", Members: []MemberChoice{{Member: "физика", Action: MemberSkip}, {Member: "физики", Action: MemberSkip}}},
			},
			creates: []string{
				"Математический анализ: [Математический анализ (лек.) Матанализ, сем. Матан]",
				"Информатика: [Информатика лаб.]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Apply(fixture(), tt.choices)
			if err != nil {
				t.Fatal(err)
			}
			var creates, additions []string
			for _, c := range out.Creates {
				creates = append(creates, c.Name+": "+format(c.Aliases))
			}
			for _, a := range out.Additions {
				additions = append(additions, a.Name+": "+format(a.Aliases))
			}
			if !reflect.DeepEqual(creates, tt.creates) || !reflect.DeepEqual(additions, tt.additions) {
				t.Fatalf("creates %q\nwant    %q\nadditions %q\nwant      %q", creates, tt.creates, additions, tt.additions)
			}
		})
	}
}

func format(ss []string) string {
	out := "["
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out + "]"
}

func TestApplyRejects(t *testing.T) {
	tests := []struct {
		name    string
		choices []Choice
		want    error
	}{
		{"unknown group", []Choice{{Group: "химия"}}, ErrUnknownGroup},
		{"unknown member", []Choice{{Group: "информатика", Members: []MemberChoice{{Member: "физика"}}}}, ErrUnknownMember},
		{"repeated group", []Choice{{Group: "информатика"}, {Group: "информатика"}}, ErrInvalidChoice},
		{"repeated member", []Choice{{Group: "физика", Members: []MemberChoice{{Member: "физики"}, {Member: "физики"}}}}, ErrInvalidChoice},
		{"an obvious member separately", []Choice{{Group: "математический анализ", Members: []MemberChoice{
			{Member: "математический анализ", Action: MemberSeparately},
		}}}, ErrInvalidChoice},
		{"a name for a merged member", []Choice{{Group: "математический анализ", Members: []MemberChoice{
			{Member: "матан", Name: "Матан"},
		}}}, ErrInvalidChoice},
		{"create a group of a subject", []Choice{{Group: "физика", Action: GroupCreate}}, ErrInvalidChoice},
		{"add a new group to a subject", []Choice{{Group: "информатика", Action: GroupAddToSubject}}, ErrInvalidChoice},
		{"rename a group of a subject", []Choice{{Group: "физика", Name: "Физика 2"}}, ErrInvalidChoice},
		{"rename a skipped group", []Choice{{Group: "информатика", Action: GroupSkip, Name: "Инф"}}, ErrInvalidChoice},
		{"unknown group action", []Choice{{Group: "информатика", Action: GroupAction(9)}}, ErrInvalidChoice},
		{"unknown member action", []Choice{{Group: "информатика", Members: []MemberChoice{
			{Member: "информатика", Action: MemberAction(-1)},
		}}}, ErrInvalidChoice},
		{"a blank name", []Choice{{Group: "информатика", Name: "   "}}, subject.ErrInvalidName},
		{"a name of an existing subject", []Choice{{Group: "информатика", Name: "история россии"}}, ErrNameTaken},
		{"a name of another group", []Choice{{Group: "информатика", Name: "Математический анализ"}}, ErrNameTaken},
		{"a name of a spelling of a subject", []Choice{{Group: "информатика", Name: "Физики"}}, ErrNameTaken},
		{"a separate member named as its group", []Choice{{Group: "математический анализ", Members: []MemberChoice{
			{Member: "матан", Action: MemberSeparately, Name: "Математический анализ"},
		}}}, ErrNameTaken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(fixture(), tt.choices)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Apply = %v, want %v", err, tt.want)
			}
			for _, r := range err.Error() {
				if unicode.Is(unicode.Cyrillic, r) {
					t.Fatalf("error %q holds schedule text (ADR-0022)", err)
				}
			}
		})
	}
}

func TestApplyNeedsAProposalOfPropose(t *testing.T) {
	built := fixture()
	tests := []struct {
		name string
		p    Proposal
		want error
	}{
		{"hand-built groups", Proposal{Groups: built.Groups}, ErrProposalNotBuilt},
		{"a copy of a built proposal", built, nil},
		{"zero value: nothing to apply", Proposal{}, nil},
		{"built from no rows", Propose(nil, nil), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Apply(tt.p, nil); !errors.Is(err, tt.want) {
				t.Fatalf("Apply = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateAliasesAreCapped(t *testing.T) {
	spelling := func(i int) string { return "Химия" + strings.Repeat(" -", i+1) + " лек" }
	rows := []Row{{Name: "Химия", Lessons: lec(100)}}
	for i := range subject.MaxAliases + 5 {
		// Spellings with one key once the mark is cut: one obvious group, the more lessons the earlier.
		rows = append(rows, Row{Name: spelling(i), Lessons: lec(50 - i)})
	}
	out, err := Apply(Propose(rows, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Creates) != 1 {
		t.Fatalf("creates %d subjects, want 1", len(out.Creates))
	}
	c := out.Creates[0]
	if c.Name != "Химия" || len(c.Aliases) != subject.MaxAliases {
		t.Fatalf("create %q with %d aliases, want «Химия» with %d", c.Name, len(c.Aliases), subject.MaxAliases)
	}
	if c.Aliases[0] != spelling(0) || c.Aliases[subject.MaxAliases-1] != spelling(subject.MaxAliases-1) {
		t.Fatalf("aliases are not the most used first: %q", c.Aliases)
	}
}

func TestDeriveSubjectID(t *testing.T) {
	a := DeriveSubjectID("token-1", "математический анализ")
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := subject.SystemFolderID(a, subject.Systems()[0]).Validate(); err != nil {
		t.Fatalf("folder id of a derived subject id: %v", err)
	}
	if b := DeriveSubjectID("token-1", "математический анализ"); b != a {
		t.Fatalf("same token and key: %s != %s", b, a)
	}
	others := []struct{ token, key string }{
		{"token-2", "математический анализ"},
		{"token-1", "физика"},
		{"", "математический анализ"},
		{"token-1м", "атематический анализ"},
	}
	seen := map[string]bool{string(a): true}
	for _, o := range others {
		id := DeriveSubjectID(o.token, o.key)
		if seen[string(id)] {
			t.Fatalf("DeriveSubjectID(%q, %q) repeats %s", o.token, o.key, id)
		}
		seen[string(id)] = true
	}
}
