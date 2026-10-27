package subjectmatch

import (
	"reflect"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func testIndex() Index {
	return NewIndex([]Entry{
		{ID: "phys", Name: "Физика", Aliases: []string{"Общая физика", "Физ-ра?"}},
		{ID: "chem", Name: "Химия", Aliases: []string{"Орг. химия"}},
		{ID: "warm", Name: "Тёплые сети"},
		// Two subjects with the same alias: the alias is ambiguous and matches neither.
		{ID: "ma1", Name: "Матанализ 1", Aliases: []string{"Матан"}},
		{ID: "ma2", Name: "Матанализ 2", Aliases: []string{"матан"}},
	})
}

func TestResolve(t *testing.T) {
	ix := testIndex()
	tests := []struct {
		in     string
		want   shared.SubjectID
		wantOK bool
	}{
		{"Физика", "phys", true},
		{"ФИЗИКА", "phys", true},
		{"  физика  ", "phys", true},
		{"общая   физика", "phys", true},
		{"ОРГ. ХИМИЯ", "chem", true},
		{"теплые сети", "warm", true}, // «е» for «ё»
		{"ТЕПЛЫЕ\tСЕТИ", "warm", true},
		{"Тёплые сети", "warm", true},
		{"Матан", "", false}, // ambiguous
		{"Матанализ 2", "ma2", true},
		{"Физика 2", "", false},
		{"", "", false},
		{"   ", "", false},
	}
	for _, tt := range tests {
		m, ok := ix.Resolve(tt.in)
		if ok != tt.wantOK || m.ID != tt.want {
			t.Errorf("Resolve(%q) = %+v, %v; want %q, %v", tt.in, m, ok, tt.want, tt.wantOK)
		}
	}
	if m, _ := ix.Resolve("общая физика"); m.Name != "Физика" {
		t.Fatalf("Resolve names the subject %q, want its name", m.Name)
	}
	if _, ok := (Index{}).Resolve("Физика"); ok {
		t.Fatal("the zero index matched")
	}
}

// Lookup tells the four outcomes apart; only Matched carries a subject.
func TestLookup(t *testing.T) {
	ix := testIndex()
	tests := []struct {
		in   string
		want Resolution
	}{
		{"Физика", Resolution{Match: Match{ID: "phys", Name: "Физика"}, Outcome: Matched}},
		{"общая физика", Resolution{Match: Match{ID: "phys", Name: "Физика"}, Outcome: Matched}},
		{"Матан", Resolution{Outcome: Ambiguous}},
		{"МАТАН ", Resolution{Outcome: Ambiguous}},
		{"Физика 2", Resolution{Outcome: NoMatch}},
		{"", Resolution{Outcome: NoKey}},
		{" \t ", Resolution{Outcome: NoKey}},
	}
	for _, tt := range tests {
		if got := ix.Lookup(tt.in); got != tt.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	if got := (Index{}).Lookup("Физика"); got != (Resolution{Outcome: NoMatch}) {
		t.Fatalf("the zero index: Lookup = %+v", got)
	}
}

func TestTakenByOther(t *testing.T) {
	ix := testIndex()
	tests := []struct {
		in     string
		except shared.SubjectID
		want   bool
	}{
		{"физика", "", true},
		{"физика", "phys", false}, // its own name
		{"Общая Физика", "chem", true},
		{"матан", "ma1", true}, // also an alias of ma2
		{"Биология", "", false},
	}
	for _, tt := range tests {
		if got := ix.TakenByOther(tt.in, tt.except); got != tt.want {
			t.Errorf("TakenByOther(%q, %q) = %v, want %v", tt.in, tt.except, got, tt.want)
		}
	}
}

// Suggestions are the strings of no subject, merged by key; nothing matched or ambiguous is suggested.
func TestSuggest(t *testing.T) {
	got := testIndex().Suggest([]Candidate{
		{Name: "Физика", Lessons: 10},                 // matched
		{Name: "общая физика", Recordings: 2},         // matched by alias
		{Name: "Матан", Lessons: 4},                   // ambiguous: not new
		{Name: "История", Lessons: 3},                 // new
		{Name: "ИСТОРИЯ ", Lessons: 1, Recordings: 1}, // the same key: merged
		{Name: "Ёмкости", Lessons: 1},                 // new, «ё»
		{Name: "емкости", Recordings: 1},              // merged with «Ёмкости»
		{Name: "  ", Lessons: 7},                      // empty
		{Name: "Английский", Lessons: 5},
	})
	want := []Suggestion{ // the most used first, a tie by name
		{Name: "Английский", Lessons: 5},
		{Name: "История", Lessons: 4, Recordings: 1},
		{Name: "Ёмкости", Lessons: 1, Recordings: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Suggest = %+v\nwant %+v", got, want)
	}
	if got := (Index{}).Suggest(nil); len(got) != 0 {
		t.Fatalf("Suggest(nil) = %+v", got)
	}
}
