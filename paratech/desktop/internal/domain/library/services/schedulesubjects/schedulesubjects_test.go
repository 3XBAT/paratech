package schedulesubjects

import (
	"reflect"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
)

func lec(n int) Kinds { return Kinds{Lecture: n} }
func sem(n int) Kinds { return Kinds{Seminar: n} }
func lab(n int) Kinds { return Kinds{Lab: n} }
func unk(n int) Kinds { return Kinds{Unknown: n} }

// group finds the group with name; it fails the test when there is none.
func group(t *testing.T, p Proposal, name string) Group {
	t.Helper()
	for _, g := range p.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("no group %q in %v", name, names(p))
	return Group{}
}

func names(p Proposal) []string {
	var out []string
	for _, g := range p.Groups {
		out = append(out, g.Name)
	}
	return out
}

func memberKeys(g Group) []string {
	var out []string
	for _, m := range g.Members {
		out = append(out, m.Key)
	}
	return out
}

func TestObviousGroup(t *testing.T) {
	p := Propose([]Row{
		{Name: "Математический анализ (лек.)", Lessons: lec(3)},
		{Name: "Математический анализ (сем.)", Lessons: sem(2)},
		{Name: "математический анализ, сем.", Lessons: sem(1)},
		{Name: " Математический  анализ ", Recordings: 2},
		{Name: "Физика - лабораторная", Lessons: unk(2)},
		{Name: "Физика (лек.)", Lessons: lec(1), Recordings: 1},
		{Name: "Ёлки-палки", Lessons: sem(1)},
		{Name: "ЕЛКИ-ПАЛКИ (сем.)", Lessons: sem(1)},
		{Name: "   ", Lessons: lec(5)},
	}, nil)

	if got, want := names(p), []string{"Математический анализ", "Физика", "Ёлки-палки"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %q, want %q", got, want)
	}

	m := group(t, p, "Математический анализ")
	if want := []string{"Математический анализ (лек.)", "Математический анализ (сем.)", "математический анализ, сем."}; !reflect.DeepEqual(m.Aliases, want) {
		t.Fatalf("aliases = %q, want %q", m.Aliases, want)
	}
	if m.Kinds != (Kinds{Lecture: 3, Seminar: 3}) || m.Lessons != 6 || m.Recordings != 2 || m.Doubtful || len(m.Members) != 1 {
		t.Fatalf("group = %+v", m)
	}

	f := group(t, p, "Физика")
	if f.Kinds != (Kinds{Lecture: 1, Lab: 2}) {
		t.Fatalf("kinds = %+v: the mark gives the unknown lessons their kind", f.Kinds)
	}
	if want := []string{"Физика (лек.)", "Физика - лабораторная"}; !reflect.DeepEqual(f.Aliases, want) {
		t.Fatalf("aliases = %q, want %q", f.Aliases, want)
	}

	e := group(t, p, "Ёлки-палки")
	if want := []string{"ЕЛКИ-ПАЛКИ (сем.)"}; !reflect.DeepEqual(e.Aliases, want) {
		t.Fatalf("aliases = %q, want %q: «Ёлки-палки» is the key of the name", e.Aliases, want)
	}
}

func TestNameIsTheMostUsedSpelling(t *testing.T) {
	p := Propose([]Row{
		{Name: "ИСТОРИЯ (лек.)", Lessons: lec(1)},
		{Name: "История (сем.)", Lessons: sem(4)},
	}, nil)
	if g := group(t, p, "История"); !reflect.DeepEqual(g.Aliases, []string{"История (сем.)", "ИСТОРИЯ (лек.)"}) {
		t.Fatalf("aliases = %q", g.Aliases)
	}
}

func TestAbbreviationsJoinTheLongName(t *testing.T) {
	p := Propose([]Row{
		{Name: "Математический анализ (лек.)", Lessons: lec(2)},
		{Name: "Матанализ, сем.", Lessons: sem(3)},
		{Name: "Матанализ (лек.)", Lessons: lec(1)},
		{Name: "Матан", Recordings: 9},
		{Name: "Теория вероятностей и математическая статистика", Lessons: lec(2)},
		{Name: "ТВиМС (пр.)", Lessons: sem(2)},
		{Name: "Иностранный язык (практика)", Lessons: sem(4)},
		{Name: "Ин. яз.", Recordings: 1},
		{Name: "Математика", Lessons: lec(1)},
	}, nil)

	m := group(t, p, "Математический анализ")
	if want := []string{"математический анализ", "матан", "матанализ"}; !reflect.DeepEqual(memberKeys(m), want) {
		t.Fatalf("members = %q, want %q", memberKeys(m), want)
	}
	for _, mem := range m.Members[1:] {
		if !mem.Doubtful || mem.Link != LinkAbbreviation {
			t.Fatalf("member %q = %+v, want a doubtful abbreviation", mem.Key, mem)
		}
	}
	if m.Members[0].Doubtful || !m.Doubtful {
		t.Fatalf("lead %+v, group doubtful %v", m.Members[0], m.Doubtful)
	}
	if want := []string{"Математический анализ (лек.)", "Матан", "Матанализ, сем.", "Матанализ (лек.)"}; !reflect.DeepEqual(m.Aliases, want) {
		t.Fatalf("aliases = %q, want %q", m.Aliases, want)
	}
	if m.Kinds != (Kinds{Lecture: 3, Seminar: 3}) || m.Recordings != 9 {
		t.Fatalf("kinds %+v recordings %d", m.Kinds, m.Recordings)
	}

	tv := group(t, p, "Теория вероятностей и математическая статистика")
	if want := []string{"теория вероятностей и математическая статистика", "твимс"}; !reflect.DeepEqual(memberKeys(tv), want) {
		t.Fatalf("members = %q", memberKeys(tv))
	}
	if ij := group(t, p, "Иностранный язык"); len(ij.Members) != 2 || ij.Members[1].Name != "Ин. яз." {
		t.Fatalf("members = %+v", ij.Members)
	}
	// «Математика» is another subject than «Математический анализ».
	if g := group(t, p, "Математика"); len(g.Members) != 1 || g.Doubtful {
		t.Fatalf("Математика = %+v", g)
	}
}

func TestTypoJoinsTheMoreUsed(t *testing.T) {
	p := Propose([]Row{
		{Name: "Програмирование", Lessons: lab(1)},
		{Name: "Программирование (лаб.)", Lessons: lab(5)},
		{Name: "Физика", Lessons: lec(3)},
		{Name: "Физкультура", Lessons: sem(3)},
		{Name: "Микроэкономика", Lessons: lec(2)},
		{Name: "Макроэкономика", Lessons: lec(2)},
		{Name: "Физика 1", Lessons: lec(1)},
		{Name: "Физика 2", Lessons: lec(1)},
	}, nil)

	g := group(t, p, "Программирование")
	if len(g.Members) != 2 || g.Members[1].Link != LinkTypo || !g.Members[1].Doubtful {
		t.Fatalf("members = %+v", g.Members)
	}
	for _, name := range []string{"Физика", "Физкультура", "Микроэкономика", "Макроэкономика", "Физика 1", "Физика 2"} {
		if g := group(t, p, name); len(g.Members) != 1 {
			t.Fatalf("%s has members %q", name, memberKeys(g))
		}
	}
}

// TestNoChains checks that a doubtful link joins only a lead and only when exactly one fits.
func TestNoChains(t *testing.T) {
	p := Propose([]Row{
		{Name: "Основы программирования", Lessons: lec(2)},
		{Name: "Операционные платформы", Lessons: lec(2)},
		{Name: "ОП", Lessons: sem(1)},
		// «Физикк» is a typo of «Физика»; «Физикко» is one of «Физикк» only, a member: it does not follow the chain.
		{Name: "Физика", Lessons: lec(5)},
		{Name: "Физикк", Lessons: lec(2)},
		{Name: "Физикко", Lessons: lec(1)},
	}, nil)

	if g := group(t, p, "ОП"); len(g.Members) != 1 {
		t.Fatalf("ОП fits two leads and stays alone: %q", memberKeys(g))
	}
	if g := group(t, p, "Физика"); !reflect.DeepEqual(memberKeys(g), []string{"физика", "физикк"}) {
		t.Fatalf("members = %q", memberKeys(g))
	}
	if !isTypo("физикко", "физикк") || isTypo("физикко", "физика") {
		t.Fatal("the fixture needs a chain")
	}
	if g := group(t, p, "Физикко"); len(g.Members) != 1 {
		t.Fatalf("members = %q", memberKeys(g))
	}
}

func TestExistingSubjects(t *testing.T) {
	subjects := []subjectmatch.Entry{
		{ID: "s-math", Name: "Математический анализ", Aliases: []string{"Матан"}},
		{ID: "s-phys", Name: "Физика"},
		{ID: "s-chem1", Name: "Химия (лек.)"},
		{ID: "s-chem2", Name: "Химия", Aliases: []string{"Хим"}},
		{ID: "s-a", Name: "Английский", Aliases: []string{"Яз"}},
		{ID: "s-b", Name: "Немецкий", Aliases: []string{"яз"}},
		{ID: "s-op1", Name: "Основы программирования"},
		{ID: "s-op2", Name: "Операционные платформы"},
	}
	p := Propose([]Row{
		{Name: "Физика (лаб.)", Lessons: lab(3)},
		{Name: "Физики", Lessons: lec(1)},
		{Name: "ФИЗИКА", Lessons: lec(9)},
		{Name: "Матанализ, сем.", Lessons: sem(3)},
		{Name: "Матан", Recordings: 4},
		{Name: "Химия, сем.", Lessons: sem(2)},
		{Name: "Яз", Lessons: sem(1)},
		{Name: "ОП", Lessons: sem(1)},
		{Name: "ТВиМС", Lessons: sem(1)},
	}, subjects)

	if got, want := names(p), []string{"Физика", "Математический анализ", "ОП", "ТВиМС"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %q, want %q: matched, ambiguous and same-key-of-two strings stay out", got, want)
	}

	f := group(t, p, "Физика")
	if f.Subject != "s-phys" || !reflect.DeepEqual(memberKeys(f), []string{"физика", "физики"}) {
		t.Fatalf("Физика = %+v", f)
	}
	if f.Members[0].Doubtful || f.Members[1].Link != LinkTypo || !f.Members[1].Doubtful {
		t.Fatalf("members = %+v", f.Members)
	}
	if want := []string{"Физика (лаб.)", "Физики"}; !reflect.DeepEqual(f.Aliases, want) {
		t.Fatalf("aliases = %q, want %q", f.Aliases, want)
	}

	m := group(t, p, "Математический анализ")
	if m.Subject != "s-math" || len(m.Members) != 1 || m.Members[0].Link != LinkAbbreviation || !m.Doubtful {
		t.Fatalf("Математический анализ = %+v", m)
	}
	if g := group(t, p, "ОП"); g.Subject != "" {
		t.Fatalf("ОП is near two subjects and is proposed as new: %+v", g)
	}
}

func TestRepeatedSubjectIDUnionsKeys(t *testing.T) {
	// As subjectmatch.NewIndex: the keys of both entries, the name of the last one.
	p := Propose([]Row{{Name: "Химии", Lessons: lec(1)}}, []subjectmatch.Entry{
		{ID: "s1", Name: "Химия"},
		{ID: "s1", Name: "Общая химия"},
	})
	if len(p.Groups) != 1 || p.Groups[0].Subject != "s1" || p.Groups[0].Name != "Общая химия" {
		t.Fatalf("groups = %+v", p.Groups)
	}
}

func TestProposeIsDeterministic(t *testing.T) {
	rows := []Row{
		{Name: "Математический анализ (лек.)", Lessons: lec(2)},
		{Name: "Матанализ, сем.", Lessons: sem(2)},
		{Name: "Матан", Recordings: 2},
		{Name: "Физика", Lessons: lec(2)},
		{Name: "Физики", Lessons: lec(2)},
		{Name: "Информатика лаб.", Lessons: lab(2)},
		{Name: "Информатика", Lessons: lec(2)},
		{Name: "ТВиМС", Lessons: sem(2)},
		{Name: "Теория вероятностей и математическая статистика", Lessons: lec(2)},
	}
	subjects := []subjectmatch.Entry{{ID: "s1", Name: "История"}, {ID: "s2", Name: "Информатика (лек.)"}}
	want := Propose(rows, subjects).Groups
	for i := range 20 {
		shuffled := slices.Clone(rows)
		for j := range shuffled {
			k := (j*7 + i*3) % len(shuffled)
			shuffled[j], shuffled[k] = shuffled[k], shuffled[j]
		}
		subs := slices.Clone(subjects)
		if i%2 == 1 {
			slices.Reverse(subs)
		}
		if got := Propose(shuffled, subs).Groups; !reflect.DeepEqual(got, want) {
			t.Fatalf("order %d: groups differ\n got %+v\nwant %+v", i, got, want)
		}
	}
	if g := Propose(rows, subjects); group(t, g, "Информатика (лек.)").Subject != "s2" {
		t.Fatal("Информатика joins the subject «Информатика (лек.)» by the key without the mark")
	}
}
