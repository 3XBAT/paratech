package agenteval

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func catalogClasses(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "shared", "testdata", "tool_catalog_v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	classes, err := CatalogClasses(raw)
	if err != nil {
		t.Fatal(err)
	}
	return classes
}

func loadCases(t *testing.T) CaseSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "live_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := ParseCaseSet(raw, catalogClasses(t))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// TestCaseSetFile keeps the live case file valid in CI: sizes, IDs, classes, catalog tools, reference citations, and
// only fake addresses.
func TestCaseSetFile(t *testing.T) {
	cs := loadCases(t)
	raw, _ := os.ReadFile(filepath.Join("testdata", "live_cases.json"))
	if strings.Contains(string(raw), "@") && !strings.Contains(string(raw), "example.invalid") {
		t.Fatal("the case file has an address that is not example.invalid")
	}
	got := map[string]int{}
	for _, r := range cs.Requests {
		got[r.Class]++
	}
	for _, c := range []string{ClassRead, ClassWrite, ClassConfirm, ClassPlan, ClassQuestions, ClassReply} {
		if got[c] == 0 {
			t.Errorf("no request of class %s", c)
		}
	}
	// A case's expected class must be reachable from its expected tools (a write case lists a write tool).
	classes := catalogClasses(t)
	for _, r := range cs.Requests {
		switch r.Class {
		case ClassRead, ClassWrite, ClassConfirm:
			if c := ClassOf(r.Tools, classes); c != r.Class {
				t.Errorf("%s: tools %v give class %s, case says %s", r.ID, r.Tools, c, r.Class)
			}
		}
	}
}

func TestParseCaseSetRejects(t *testing.T) {
	classes := catalogClasses(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "live_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"unknown field":      func(s string) string { return strings.Replace(s, `"version": 1`, `"version": 1, "extra": 1`, 1) },
		"unknown tool":       func(s string) string { return strings.Replace(s, `["create_subject"]`, `["send_email"]`, 1) },
		"card tool expected": func(s string) string { return strings.Replace(s, `["create_subject"]`, `["reply"]`, 1) },
		"unknown class":      func(s string) string { return strings.Replace(s, `"class": "reply"`, `"class": "chat"`, 1) },
		"duplicate id":       func(s string) string { return strings.Replace(s, `"id": "r02"`, `"id": "r01"`, 1) },
		"bad reference":      func(s string) string { return strings.Replace(s, `"quote": "I = U / R"`, `"quote": "I = U * R"`, 1) },
		"unknown subject": func(s string) string {
			return strings.Replace(s, `"subject": "s-math", "class"`, `"subject": "s-chem", "class"`, 1)
		},
	} {
		mutated := mutate(string(raw))
		if mutated == string(raw) {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := ParseCaseSet([]byte(mutated), classes); !errors.Is(err, ErrInvalidCaseSet) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestClassOf(t *testing.T) {
	classes := catalogClasses(t)
	for _, tt := range []struct {
		tools []string
		want  string
	}{
		{nil, ClassReply},
		{[]string{"reply"}, ClassReply},
		{[]string{"list_assignments", "reply"}, ClassRead},
		{[]string{"find_lessons", "make_summary"}, ClassWrite},
		{[]string{"find_lessons", "propose_delete_recording", "edit_due"}, ClassConfirm},
		{[]string{"list_assignments", "show_plan"}, ClassPlan},
		{[]string{"ask_questions"}, ClassQuestions},
		{[]string{"inspect_upload", "cannot_do"}, ClassCannotDo},
		{[]string{"show_plan", "ask_questions"}, ClassPlan},
	} {
		if got := ClassOf(tt.tools, classes); got != tt.want {
			t.Errorf("ClassOf(%v) = %s, want %s", tt.tools, got, tt.want)
		}
	}
}

func TestMatchTools(t *testing.T) {
	for _, tt := range []struct {
		want, got []string
		ok        bool
	}{
		{nil, nil, true},
		{nil, []string{"search"}, true},
		{[]string{"find_lessons", "make_summary"}, []string{"list_subjects", "find_lessons", "search", "make_summary", "reply"}, true},
		{[]string{"find_lessons", "make_summary"}, []string{"make_summary", "find_lessons"}, false},
		{[]string{"edit_due"}, []string{"list_assignments"}, false},
	} {
		if MatchTools(tt.want, tt.got) != tt.ok {
			t.Errorf("MatchTools(%v, %v) != %v", tt.want, tt.got, tt.ok)
		}
	}
}

func TestFirstPersonWords(t *testing.T) {
	for text, want := range map[string][]string{
		"Конспект готов.":                       nil,
		"Я сделал конспект.":                    {"я"},
		"Мне нужно уточнить: мы ждём ответа":    {"мне", "мы"},
		"Найдено 3 задания. Срок — завтра":      nil,
		"I've created the folder for my notes":  {"i've", "my"},
		"I’m done":                              {"i'm"},
		"Ящик, мясо, мыло, наши, усы, Imagine.": nil, // substrings and other words are not first person
		"Задание из Моего курса":                {"моего"},
	} {
		if got := FirstPersonWords(text); !slices.Equal(got, want) {
			t.Errorf("FirstPersonWords(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestCardTexts(t *testing.T) {
	for _, tt := range []struct {
		tool, args string
		want       []string
	}{
		{"reply", `{"text":"Готово."}`, []string{"Готово."}},
		{"show_plan", `{"summary":"План: папка.","steps":[],"notes":["Платно."]}`, []string{"План: папка.", "Платно."}},
		{"cannot_do", `{"your_part":["Войти в ВузКАД."],"our_part":["Файл .docx."]}`, []string{"Войти в ВузКАД.", "Файл .docx."}},
		{"ask_questions", `{"questions":[{"kind":"choice","question":"Объём?","options":["5","10"],"default":0}]}`, []string{"Объём?", "5", "10"}},
		{"ask_questions", `{"questions":[{"kind":"text","text":"Срок?"}]}`, []string{"Срок?"}},
		{"list_subjects", `{}`, nil},
		{"reply", `not json`, nil},
	} {
		if got := CardTexts(tt.tool, tt.args); !slices.Equal(got, tt.want) {
			t.Errorf("CardTexts(%s, %s) = %v, want %v", tt.tool, tt.args, got, tt.want)
		}
	}
}

func TestCitations(t *testing.T) {
	chunks := []Chunk{{ID: "c1", Text: "Теорема Ролля: найдётся точка c,  в которой f'(c) = 0."}}
	for _, tt := range []struct {
		c  Citation
		ok bool
	}{
		{Citation{"c1", "найдется точка c, в которой"}, true}, // ё/е and spaces do not matter
		{Citation{"c1", "НАЙДЁТСЯ ТОЧКА C"}, true},
		{Citation{"c1", "точка"}, false},                       // too short
		{Citation{"c1", "найдётся точка d, в которой"}, false}, // not in the chunk
		{Citation{"c2", "найдётся точка c, в которой"}, false}, // unknown chunk
	} {
		if CitationValid(tt.c, chunks) != tt.ok {
			t.Errorf("CitationValid(%+v) != %v", tt.c, tt.ok)
		}
	}
	if v, n := CitationShare([]Citation{{"c1", "в которой f'(c) = 0"}, {"c1", "в которой f'(c) = 1"}}, chunks); v != 1 || n != 2 {
		t.Fatalf("share = %d/%d", v, n)
	}
}

func TestReport(t *testing.T) {
	classes := catalogClasses(t)
	q := Question{ID: "k1", Text: "Что такое ряд?", Chunks: []Chunk{{ID: "c1", Text: "Ряд сходится абсолютно, если сходится ряд модулей."}}}
	r := Report{
		Classes: classes,
		Requests: []Outcome{
			{Case: Request{ID: "r1", Text: "Что задано?", Class: ClassRead, Tools: []string{"list_assignments"}},
				Tools: []string{"list_assignments", "reply"}, Texts: []string{"Найдено 2 задания."}},
			{Case: Request{ID: "w1", Text: "Создай предмет", Class: ClassWrite, Tools: []string{"create_subject"}},
				Tools: []string{"reply"}, Texts: []string{"Я создал предмет."}},
			{Case: Request{ID: "n1", Text: "Загрузи в ВузКАД", Class: ClassCannotDo}, Tools: []string{"cannot_do"}},
		},
		Questions: []QuestionOutcome{
			{Question: q, Tools: []string{"answer_question"}, Measured: true, Citations: []Citation{{"c1", "если сходится ряд модулей"}}},
			{Question: q, Tools: []string{"search"}, Err: "unavailable"},
		},
	}
	m := r.Metrics()
	want := Metrics{Requests: 2, ClassOK: 1, ToolsOK: 1, CannotDo: 1, CannotDoOK: 1, Texts: 2, FirstPerson: 1,
		Questions: 2, AnswerRouted: 1, Measured: 1, Valid: 1, Citations: 1, Errors: 1}
	if m != want {
		t.Fatalf("metrics = %+v\nwant      %+v", m, want)
	}
	md := r.Markdown()
	for _, s := range []string{"1/2 (50%)", "1/1 (100%)", "w1 «Создай предмет»", "голос не безличный [я]", "k1 «Что такое ряд?»: ждали answer_question"} {
		if !strings.Contains(md, s) {
			t.Errorf("report misses %q:\n%s", s, md)
		}
	}
	if !strings.Contains(Report{Classes: classes}.Markdown(), "не измерено") {
		t.Error("a run without measured answers must say so")
	}
}
