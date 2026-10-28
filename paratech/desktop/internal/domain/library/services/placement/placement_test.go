package placement

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// candidate is a subject with the six system folders, as subject.New makes them.
func candidate(id shared.SubjectID, name string, aliases ...string) Candidate {
	c := Candidate{ID: id, Name: name, Aliases: aliases}
	for _, sys := range subject.Systems() {
		c.Folders = append(c.Folders, Folder{ID: subject.SystemFolderID(id, sys), System: sys})
	}
	return c
}

// without drops the system folder sys of c.
func without(c Candidate, sys subject.System) Candidate {
	var fs []Folder
	for _, f := range c.Folders {
		if f.System != sys {
			fs = append(fs, f)
		}
	}
	c.Folders = fs
	return c
}

func existing(id shared.SubjectID, sys subject.System) Hint {
	return Hint{Kind: KindExisting, SubjectID: id, FolderID: subject.SystemFolderID(id, sys), FolderKind: sys}
}

func create(name string, sys subject.System, aliases ...string) Hint {
	return Hint{Kind: KindCreate, FolderKind: sys, CreateName: name, CreateAliases: aliases}
}

// names is proposals of the schedule-subjects card with no spellings but their names.
func names(ns ...string) []Proposed {
	out := make([]Proposed, 0, len(ns))
	for _, n := range ns {
		out = append(out, Proposed{Name: n})
	}
	return out
}

func TestFor(t *testing.T) {
	nets := candidate("s1", "Сети", "Компьютерные сети")
	physics := candidate("s2", "Физика")
	solid := candidate("s3", "Физика твёрдого тела")
	chem := candidate("s4", "Химия", "Хим")
	practicum := candidate("s5", "Практикум по программированию")
	all := []Candidate{nets, physics, chem, practicum}

	tests := []struct {
		name     string
		file     string
		subjects []Candidate
		schedule []Proposed
		want     Hint
	}{
		{"lab marker with a number", "Сети_лр2.docx", all, nil, existing("s1", subject.SystemLabs)},
		{"lab marker upper case after a separator", "СЕТИ-ЛР_3.pdf", all, nil, existing("s1", subject.SystemLabs)},
		{"full lab word", "Лабораторная работа 4 (сети).docx", all, nil, existing("s1", subject.SystemLabs)},
		{"seminar", "физика сем 5.pptx", all, nil, existing("s2", subject.SystemSeminars)},
		{"practice", "Физика. Практическое занятие.docx", all, nil, existing("s2", subject.SystemSeminars)},
		{"pz", "физика_пз1.txt", all, nil, existing("s2", subject.SystemSeminars)},
		{"lecture", "Лекция 3 Физика.pdf", all, nil, existing("s2", subject.SystemLectures)},
		{"tickets", "Билеты по химии — Химия.docx", all, nil, existing("s4", subject.SystemTickets)},
		{"homework short", "химия ДЗ7.docx", all, nil, existing("s4", subject.SystemHomework)},
		{"homework word", "Домашнее задание (химия).docx", all, nil, existing("s4", subject.SystemHomework)},
		{"no marker is materials", "Физика.pdf", all, nil, existing("s2", subject.SystemMaterials)},
		{"two markers of two folders is materials", "Физика лекция ДЗ.docx", all, nil, existing("s2", subject.SystemMaterials)},
		{"extension does not choose the folder", "Физика.pptx", all, nil, existing("s2", subject.SystemMaterials)},
		{"yo read as ye", "ФИЗИКА ТВЕРДОГО ТЕЛА лек.pdf", []Candidate{physics, solid}, nil, existing("s3", subject.SystemLectures)},
		{"longest match wins", "Физика_твёрдого_тела.docx", []Candidate{physics, solid}, nil, existing("s3", subject.SystemMaterials)},
		{"alias", "Компьютерные сети, лаб.docx", all, nil, existing("s1", subject.SystemLabs)},
		{"short alias", "хим_лр1.docx", all, nil, existing("s4", subject.SystemLabs)},
		{"digits are boundaries", "2026физика1.pdf", all, nil, existing("s2", subject.SystemMaterials)},
		{"directory part is dropped", `..\..\Физика\лр.docx`, all, nil, Hint{}},
		{"subject words of the match are no marker", "Практикум по программированию 3.docx", all, nil, existing("s5", subject.SystemMaterials)},
		{"part of a word does not match", "Физикатест.docx", all, nil, Hint{}},
		{"marker inside a word is none", "Физика Семёнов.docx", all, nil, existing("s2", subject.SystemMaterials)},
		{"folder missing falls back to materials", "Сети лр.docx", []Candidate{without(nets, subject.SystemLabs)}, nil, existing("s1", subject.SystemMaterials)},
		{"no materials folder either", "Сети лр.docx", []Candidate{without(without(nets, subject.SystemLabs), subject.SystemMaterials)}, nil,
			Hint{Kind: KindExisting, SubjectID: "s1", FolderKind: subject.SystemMaterials}},
		{"ambiguous subjects", "Физика.docx", []Candidate{physics, candidate("s9", "Механика", "физика")}, nil, Hint{}},
		{"ambiguous subjects are not replaced by the schedule", "Физика.docx",
			[]Candidate{physics, candidate("s9", "Механика", "физика")}, names("Физика"), Hint{}},
		{"same subject twice is no ambiguity", "Физика.docx", []Candidate{physics, physics}, nil, existing("s2", subject.SystemMaterials)},
		{"no subject", "scan_0001.pdf", all, nil, Hint{}},
		{"single letter does not match", "c_lab.txt", []Candidate{candidate("s7", "C++")}, nil, Hint{}},
		{"create by schedule", "сети_лр2.docx", []Candidate{physics}, names("Сети", "Матанализ"), create("Сети", subject.SystemLabs)},
		{"create by schedule spelled by the card", "КОМП_СЕТИ.docx", nil, names("  Комп   сети "), create("Комп сети", subject.SystemMaterials)},
		{"schedule string with a mark", "сети_лр2.docx", nil, []Proposed{{Name: "Сети", Spellings: []string{"Сети (лаб.)"}}},
			create("Сети", subject.SystemLabs, "Сети (лаб.)")},
		{"matched by a spelling, named by the card", "мат анализ сем.pdf", nil,
			[]Proposed{{Name: "Матанализ", Spellings: []string{"Мат. анализ (лек.)", "матанализ, сем."}}},
			create("Матанализ", subject.SystemSeminars, "Мат. анализ (лек.)", "матанализ, сем.")},
		{"file name never names the subject", "СЕТИ (ЛАБ.)_лр.docx", nil, []Proposed{{Name: "Сети", Spellings: []string{"Сети (лаб.)"}}},
			create("Сети", subject.SystemLabs, "Сети (лаб.)")},
		{"aliases: one per key, not the name, valid only", "сети.docx", nil, []Proposed{{Name: "Сети", Spellings: []string{
			"СЕТИ", "Сети (лаб.)", "сети (ЛАБ.)", strings.Repeat("сети ", 50), "  Сети,  сем. ",
		}}}, create("Сети", subject.SystemMaterials, "Сети (лаб.)", "Сети, сем.")},
		{"longest schedule string wins", "Компьютерные сети лек.docx", nil, names("Сети", "Компьютерные сети"),
			create("Компьютерные сети", subject.SystemLectures)},
		{"ambiguous schedule strings", "Сети Физо.docx", nil, names("Сети", "Физо"), Hint{}},
		{"schedule strings apart", "Сети Химия.docx", nil, names("Сети", "Химия"), Hint{}},
		{"subjects apart", "Физика и химия.docx", all, nil, Hint{}},
		{"subject twice apart", "Физика лек физика.docx", all, nil, existing("s2", subject.SystemLectures)},
		{"shorter subject again apart is a part of the longer", "Физика лек Физика твёрдого тела.docx", []Candidate{physics, solid}, nil,
			existing("s3", subject.SystemLectures)},
		{"shorter subject first and after", "Физика твёрдого тела, физика.docx", []Candidate{physics, solid}, nil,
			existing("s3", subject.SystemMaterials)},
		{"another subject apart still ambiguous", "Химия лек Физика твёрдого тела.docx", []Candidate{physics, solid, chem}, nil, Hint{}},
		{"equally long subjects at two places", "Физика лек физика.docx", []Candidate{physics, candidate("s9", "Механика", "физика")}, nil, Hint{}},
		{"shorter schedule string again apart", "Сети лр Компьютерные сети.docx", nil, names("Сети", "Компьютерные сети"),
			create("Компьютерные сети", subject.SystemLabs)},
		{"same schedule key twice", "сети.docx", nil, names("Сети", "СЕТИ"), create("Сети", subject.SystemMaterials)},
		{"subject wins over schedule", "Физика сети.docx", []Candidate{physics}, names("Компьютерные сети", "Сети"), existing("s2", subject.SystemMaterials)},
		{"invalid schedule name is skipped", "сети.docx", nil, names(strings.Repeat("сети ", 50)), Hint{}},
		{"invalid name with a valid spelling is skipped", "сети.docx", nil,
			[]Proposed{{Name: strings.Repeat("сети ", 50), Spellings: []string{"Сети"}}}, Hint{}},
		{"empty name", "", all, names("Сети"), Hint{}},
		{"only an extension", ".docx", all, nil, Hint{}},
		{"html", `<img src=x onerror=alert(1)>.docx`, all, names("Сети"), Hint{}},
		{"rtl and control chars", "\u202eфизика\u202c\x00\x07.docx", all, nil, existing("s2", subject.SystemMaterials)},
		{"rtl inside a word splits nothing", "физ\u202eика.docx", all, nil, existing("s2", subject.SystemMaterials)},
		{"invalid utf-8", "\xff\xfeфиз\xffика.docx", all, nil, existing("s2", subject.SystemMaterials)},
		{"very long", strings.Repeat("а_", 100000) + "Физика.docx", all, nil, Hint{}},
		{"no subjects and no schedule", "Физика.docx", nil, nil, Hint{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := For(tt.file, tt.subjects, tt.schedule); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("For(%q) = %+v; want %+v", tt.file, got, tt.want)
			}
		})
	}
}

func TestForCapsAliases(t *testing.T) {
	var spellings []string
	for i := range subject.MaxAliases + 5 {
		spellings = append(spellings, fmt.Sprintf("Сети %d", i))
	}
	h := For("сети.docx", nil, []Proposed{{Name: "Сети", Spellings: spellings}})
	if h.Kind != KindCreate || !reflect.DeepEqual(h.CreateAliases, spellings[:subject.MaxAliases]) {
		t.Fatalf("For = %+v; want the first %d spellings", h, subject.MaxAliases)
	}
}

func TestTokens(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"ЛР_3", []string{"лр", "3"}},
		{"лр2", []string{"лр", "2"}},
		{"Ёлка-(Сети).v2", []string{"елка", "сети", "v", "2"}},
		{"и\u0306од", []string{"и\u0306од"}},
		{"", nil},
		{"_-.()", nil},
	}
	for _, tt := range tests {
		if got := tokens(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("tokens(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestStem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Сети.docx", "Сети"},
		{`C:\dir\Сети.docx`, "Сети"},
		{"a/b/Сети", "Сети"},
		{"Лекция 1. Введение в сети", "Лекция 1. Введение в сети"},
		{".docx", ".docx"},
		{"Сети.", "Сети."},
	}
	for _, tt := range tests {
		if got := stem(tt.in); got != tt.want {
			t.Errorf("stem(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func FuzzFor(f *testing.F) {
	for _, s := range []string{"Сети_лр2.docx", "", "\x00", `..\..\a.docx`, "лек.лек.лек", "\u202e"} {
		f.Add(s)
	}
	subjects := []Candidate{candidate("s1", "Сети"), candidate("s2", "Физика", "физ")}
	schedule := []Proposed{{Name: "Химия", Spellings: []string{"Химия (лек.)"}}}
	f.Fuzz(func(t *testing.T, name string) {
		h := For(name, subjects, schedule)
		switch h.Kind {
		case KindNone:
			if !reflect.DeepEqual(h, Hint{}) {
				t.Fatalf("none with fields: %+v", h)
			}
		case KindExisting:
			if h.SubjectID == "" || h.CreateName != "" || h.CreateAliases != nil {
				t.Fatalf("existing: %+v", h)
			}
		case KindCreate:
			if h.CreateName != "Химия" || h.SubjectID != "" || !reflect.DeepEqual(h.CreateAliases, []string{"Химия (лек.)"}) {
				t.Fatalf("create: %+v", h)
			}
		}
	})
}
