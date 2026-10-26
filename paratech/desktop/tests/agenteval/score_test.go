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
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "shared", "testdata", CatalogFile))
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
		"list without list_assignments": func(s string) string {
			return strings.Replace(s, `"class": "reply", "tools": []}`, `"class": "reply", "tools": [], "lists_assignments": true}`, 1)
		},
		"unknown alternative class": func(s string) string { return strings.Replace(s, `"also": ["read", "reply"]`, `"also": ["chat"]`, 1) },
		"alternative equal to the class": func(s string) string {
			return strings.Replace(s, `"also": ["read", "reply"]`, `"also": ["questions"]`, 1)
		},
		"duplicate alternative": func(s string) string {
			return strings.Replace(s, `"also": ["read", "reply"]`, `"also": ["read", "read"]`, 1)
		},
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

func TestAddressWords(t *testing.T) {
	for text, want := range map[string][]string{
		"Две работы со сроком до среды.":          nil,
		"Привет! Ты можешь посмотреть черновик.":  {"привет", "ты"},
		"Скажите, какой предмет вы имеете в виду": {"вы"},
		"Черновик — в вашем разделе «Задания».":   {"вашем"},
		"Ваша работа готова":                      {"ваша"},
		"Тыква, выход, вас-то, hint, history":     {"вас"},
		"Hello, your draft is ready":              {"hello", "your"},
	} {
		if got := AddressWords(text); !slices.Equal(got, want) {
			t.Errorf("AddressWords(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestReplyAssignments(t *testing.T) {
	for _, tt := range []struct {
		tool, args string
		want       []string
	}{
		{"reply", `{"text":"Два.","assignments":["a-12","a-20"]}`, []string{"a-12", "a-20"}},
		{"reply", `{"text":"Нет."}`, nil},
		{"reply", `{"text":"x","assignments":"a-12"}`, nil},
		{"cannot_do", `{"assignments":["a-12"]}`, nil},
	} {
		if got := ReplyAssignments(tt.tool, tt.args); !slices.Equal(got, tt.want) {
			t.Errorf("ReplyAssignments(%s, %s) = %v, want %v", tt.tool, tt.args, got, tt.want)
		}
	}
}

// TestBetaQuestionsInTheCaseSet: the four questions of the beta (TASK-172/173) and a work on a topic without an
// assignment stay in the live set with the routing the v3 prompt asks for. Without an assignment the prompt allows
// questions or an honest reply (after list_assignments that is a read).
func TestBetaQuestionsInTheCaseSet(t *testing.T) {
	cs := loadCases(t)
	want := map[string][]string{
		"b01": {ClassReply}, "b02": {ClassReply}, "b03": {ClassRead}, "b04": {ClassQuestions},
		"b05": {ClassQuestions, ClassRead, ClassReply},
	}
	for id, classes := range want {
		i := slicesIndex(cs.Requests, id)
		if i < 0 {
			t.Errorf("%s: missing", id)
			continue
		}
		r := cs.Requests[i]
		if r.Class != classes[0] || len(r.Also) != len(classes)-1 {
			t.Errorf("%s: classes %s, want %v", id, r.Expected(), classes)
		}
		for _, c := range classes {
			if !r.Accepts(c) {
				t.Errorf("%s: does not accept %s", id, c)
			}
		}
	}
	if i := slicesIndex(cs.Requests, "b05"); i >= 0 && cs.Requests[i].Accepts(ClassPlan) {
		t.Error("b05 must not accept a plan without an assignment")
	}
	if i := slicesIndex(cs.Requests, "b03"); i < 0 || !cs.Requests[i].ListsAssignments {
		t.Error("b03 must expect a reply with the list of assignments")
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

	// Catalog v3: a reply that should list the assignments and a greeting with an address are both reported.
	v3 := Report{Classes: classes, Requests: []Outcome{
		{Case: Request{ID: "b03", Text: "Что горит?", Class: ClassRead, Tools: []string{"list_assignments"}, ListsAssignments: true},
			Tools: []string{"list_assignments", "reply"}, Texts: []string{"Привет! У тебя два задания."}},
		{Case: Request{ID: "b03b", Text: "Что на неделе?", Class: ClassRead, Tools: []string{"list_assignments"}, ListsAssignments: true},
			Tools: []string{"list_assignments", "reply"}, Texts: []string{"Два задания."}, Listed: []string{"a-12", "a-20"}},
	}}
	if m := v3.Metrics(); m.Lists != 2 || m.ListsOK != 1 || m.Address != 1 || m.Texts != 2 {
		t.Fatalf("v3 metrics = %+v", m)
	}
	md = v3.Markdown()
	for _, s := range []string{"| reply со списком заданий | 1/2 (50%) |", "b03 «Что горит?»: reply без списка заданий", "b03: обращение [привет тебя]"} {
		if !strings.Contains(md, s) {
			t.Errorf("report misses %q:\n%s", s, md)
		}
	}

	// An alternative class counts as routed; a class outside them is reported with every expected class.
	b05 := Request{ID: "b05", Text: "Презентация", Class: ClassQuestions, Also: []string{ClassRead, ClassReply}, Tools: []string{"list_assignments"}}
	alt := Report{Classes: classes, Requests: []Outcome{
		{Case: b05, Tools: []string{"list_assignments", "ask_questions"}},
		{Case: b05, Tools: []string{"list_assignments", "reply"}},
		{Case: b05, Tools: []string{"list_assignments", "show_plan"}},
	}}
	if m := alt.Metrics(); m.Requests != 3 || m.ClassOK != 2 || m.ToolsOK != 3 {
		t.Fatalf("alternative metrics = %+v", m)
	}
	if md := alt.Markdown(); !strings.Contains(md, "ждали questions/read/reply [list_assignments], получили plan") ||
		strings.Count(md, "b05 «") != 1 {
		t.Errorf("alternative report:\n%s", md)
	}
}
