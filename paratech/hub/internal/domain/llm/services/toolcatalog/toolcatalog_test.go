package toolcatalog

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"paratech/hub/internal/domain/llm/values"
)

// golden is the contract's current catalog (v3); the Hub embeds a byte-identical copy (hub/pkg/prompts). goldenV1 and
// goldenV2 are frozen, still served to clients of those versions.
const (
	golden   = "../../../../../../shared/testdata/tool_catalog_v3.json"
	goldenV2 = "../../../../../../shared/testdata/tool_catalog_v2.json"
	goldenV1 = "../../../../../../shared/testdata/tool_catalog_v1.json"
)

func goldenCatalog(t *testing.T) *Catalog {
	t.Helper()
	return parseFile(t, golden)
}

func parseFile(t *testing.T, file string) *Catalog {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestQueryArguments: catalog v2 bounds the question of answer_question and the query of search at MaxQueryRunes on
// one line; v1 keeps them labels of MaxLabelRunes.
func TestQueryArguments(t *testing.T) {
	v1, v2 := parseFile(t, goldenV1), parseFile(t, goldenV2)
	if v1.Version() != 1 || v2.Version() != 2 {
		t.Fatalf("versions %d and %d", v1.Version(), v2.Version())
	}
	for _, tc := range []struct {
		tool, arg string
	}{{"search", "query"}, {"answer_question", "question"}} {
		at := func(n int, r string) string { return `{"` + tc.arg + `":"` + strings.Repeat(r, n) + `"}` }
		if err := v2.Check(call(t, tc.tool, at(MaxQueryRunes, "я"))); err != nil {
			t.Errorf("v2 %s at the limit: %v", tc.tool, err)
		}
		for name, args := range map[string]string{
			"long":    at(MaxQueryRunes+1, "я"),
			"newline": `{"` + tc.arg + `":"a\nb"}`,
			"blank":   `{"` + tc.arg + `":"   "}`,
		} {
			if err := v2.Check(call(t, tc.tool, args)); !errors.Is(err, ErrInvalidCall) {
				t.Errorf("v2 %s %s: err = %v, want ErrInvalidCall", tc.tool, name, err)
			}
		}
		if err := v1.Check(call(t, tc.tool, at(MaxLabelRunes+1, "я"))); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("v1 %s over the label bound: err = %v, want ErrInvalidCall", tc.tool, err)
		}

		var schema map[string]any
		if err := json.Unmarshal([]byte(specFor(t, v2, tc.tool).Schema()), &schema); err != nil {
			t.Fatal(err)
		}
		if p := schema["properties"].(map[string]any)[tc.arg].(map[string]any); p["maxLength"] != float64(MaxQueryRunes) ||
			!strings.Contains(p["description"].(string), "500") {
			t.Errorf("v2 %s schema = %v", tc.tool, p)
		}
	}
}

// TestReplyAssignments: catalog v3 lets reply list 0-MaxCardIDs assignment IDs (TASK-172); v2 has no such argument.
func TestReplyAssignments(t *testing.T) {
	v2, v3 := parseFile(t, goldenV2), goldenCatalog(t)
	ids := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = `"asg-` + strings.Repeat("x", i+1) + `"`
		}
		return `{"text":"Три задания.","assignments":[` + strings.Join(items, ",") + `]}`
	}
	for _, args := range []string{ids(0), ids(1), ids(MaxCardIDs), `{"text":"Заданий нет."}`} {
		if err := v3.Check(call(t, "reply", args)); err != nil {
			t.Errorf("v3 reply %s: %v", args, err)
		}
	}
	for name, args := range map[string]string{
		"eleven":     ids(MaxCardIDs + 1),
		"not an id":  `{"text":"t","assignments":["a 1"]}`,
		"empty id":   `{"text":"t","assignments":[""]}`,
		"long id":    `{"text":"t","assignments":["` + strings.Repeat("a", 129) + `"]}`,
		"a number":   `{"text":"t","assignments":[7]}`,
		"an object":  `{"text":"t","assignments":[{"id":"a-1"}]}`,
		"not a list": `{"text":"t","assignments":"a-1"}`,
		"null":       `{"text":"t","assignments":null}`,
	} {
		if err := v3.Check(call(t, "reply", args)); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("v3 reply %s: err = %v, want ErrInvalidCall", name, err)
		}
	}
	if err := v2.Check(call(t, "reply", ids(1))); !errors.Is(err, ErrInvalidCall) {
		t.Errorf("v2 reply with assignments: err = %v, want ErrInvalidCall", err)
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(specFor(t, v3, "reply").Schema()), &schema); err != nil {
		t.Fatal(err)
	}
	p := schema["properties"].(map[string]any)["assignments"].(map[string]any)
	if p["type"] != "array" || p["maxItems"] != float64(MaxCardIDs) || p["minItems"] != nil ||
		p["items"].(map[string]any)["maxLength"] != float64(maxIDBytes) || len(schema["required"].([]any)) != 1 {
		t.Errorf("v3 reply schema = %v", schema)
	}
}

func call(t *testing.T, tool, args string) values.ToolCall {
	t.Helper()
	c, err := values.NewToolCall("toolu_1", tool, args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseGolden(t *testing.T) {
	c := goldenCatalog(t)
	if c.Version() != 3 || len(c.Specs()) != 27|| !c.Has("show_plan") || c.Has("send_email") {
		t.Fatalf("catalog version %d, %d tools", c.Version(), len(c.Specs()))
	}
	specs := c.Specs()
	if specs[0].Name() != "list_subjects" || specs[len(specs)-1].Name() != "cannot_do" {
		t.Fatal("specs are not in catalog order")
	}
	for _, s := range specs {
		var schema map[string]any
		if err := json.Unmarshal([]byte(s.Schema()), &schema); err != nil || schema["type"] != "object" ||
			schema["additionalProperties"] != false {
			t.Errorf("%s: schema %s", s.Name(), s.Schema())
		}
		if s.Description() == "" {
			t.Errorf("%s: no description", s.Name())
		}
	}
	if d := specFor(t, c, "propose_send").Description(); !strings.Contains(d, "confirmation card") {
		t.Errorf("a C tool must say it only creates a card: %s", d)
	}
	if d := specFor(t, c, "make_summary").Description(); !strings.Contains(d, "Paid") {
		t.Errorf("a paid tool must say so: %s", d)
	}
	// The schema text is stable, so the cached prompt prefix does not change between Hub starts.
	if again := goldenCatalog(t); again.Specs()[3].Schema() != specs[3].Schema() {
		t.Fatal("schema text is not deterministic")
	}
}

func specFor(t *testing.T, c *Catalog, name string) values.ToolSpec {
	t.Helper()
	for _, s := range c.Specs() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no tool %s", name)
	return values.ToolSpec{}
}

func TestSchemaShapes(t *testing.T) {
	c := goldenCatalog(t)
	schema := func(tool string) map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(specFor(t, c, tool).Schema()), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	reply := schema("reply")
	text := reply["properties"].(map[string]any)["text"].(map[string]any)
	if text["maxLength"] != float64(MaxTextRunes) || text["type"] != "string" || reply["required"].([]any)[0] != "text" {
		t.Fatalf("reply schema = %v", reply)
	}
	search := schema("search")["properties"].(map[string]any)
	if search["query"].(map[string]any)["maxLength"] != float64(MaxQueryRunes) {
		t.Fatalf("query schema = %v", search["query"])
	}
	instruction := schema("generate_draft")["properties"].(map[string]any)["instruction"].(map[string]any)
	if instruction["maxLength"] != float64(MaxLabelRunes) {
		t.Fatalf("label schema = %v", instruction)
	}
	if enum := search["scope"].(map[string]any)["enum"].([]any); len(enum) != 2 || enum[0] != "subject" {
		t.Fatalf("enum schema = %v", search["scope"])
	}
	if list := schema("list_subjects"); len(list["properties"].(map[string]any)) != 0 || list["required"] != nil {
		t.Fatalf("no-argument schema = %v", list)
	}
	steps := schema("show_plan")["properties"].(map[string]any)["steps"].(map[string]any)
	tools := steps["items"].(map[string]any)["properties"].(map[string]any)["tool"].(map[string]any)["enum"].([]any)
	for _, name := range tools {
		if name == "reply" || name == "show_plan" {
			t.Fatalf("a plan step may name UI tool %s", name)
		}
	}
	if len(tools) != 23 {
		t.Fatalf("plan step tools = %d, want the 23 non-UI tools", len(tools))
	}
}

func TestCheckAcceptsValidCalls(t *testing.T) {
	c := goldenCatalog(t)
	for tool, args := range map[string]string{
		"list_subjects":     `{}`,
		"find_lessons":      `{"subject":"subj-1","from":"2026-10-01","to":"2026-10-31","kind":"lecture"}`,
		"job_status":        `{"ids":["job-1","job-2"]}`,
		"search":            `{"query":"` + strings.Repeat("я", MaxQueryRunes) + `"}`,
		"answer_question":   `{"question":"` + strings.Repeat("я", MaxQueryRunes) + `","scope":"all"}`,
		"create_folder":     `{"subject":"subj-1","name":"` + strings.Repeat("я", MaxLabelRunes) + `"}`,
		"edit_due":          `{"assignment":"a-1","due":"2026-12-31"}`,
		"generate_draft":    `{"assignment":"a-1"}`,
		"propose_send":      `{"assignment":"a-1","version":"v-2"}`,
		"reply":             `{"text":"Готово.\nДва задания."}`,
		"cannot_do":         `{"your_part":["Сдать в ВузКАД"],"our_part":[]}`,
		"show_plan":         `{"summary":"Конспект и поиск ДЗ.","steps":[{"tool":"make_summary","arguments":{"recording":"rec-1"}},{"tool":"list_subjects"}],"notes":["Платно."]}`,
		"ask_questions":     `{"questions":[{"kind":"choice","question":"Какой предмет?","options":["Матан","Физика"],"default":1},{"kind":"text","question":"Срок?"}]}`,
		"triage_assignment": `{"upload":"up-1"}`,
	} {
		if err := c.Check(call(t, tool, args)); err != nil {
			t.Errorf("%s %s: %v", tool, args, err)
		}
	}
}

func TestCheckRejectsInvalidCalls(t *testing.T) {
	c := goldenCatalog(t)
	long := strings.Repeat("я", MaxTextRunes+1)
	for name, tc := range map[string][2]string{
		"unknown tool":         {"send_email", `{}`},
		"undeclared argument":  {"list_subjects", `{"to":"x@y.z"}`},
		"missing required":     {"delivery_status", `{}`},
		"id not a string":      {"delivery_status", `{"assignment":5}`},
		"id with space":        {"delivery_status", `{"assignment":"a 1"}`},
		"long id":              {"delivery_status", `{"assignment":"` + strings.Repeat("a", 129) + `"}`},
		"empty ids":            {"job_status", `{"ids":[]}`},
		"ids not strings":      {"job_status", `{"ids":[1]}`},
		"bad date":             {"edit_due", `{"assignment":"a","due":"2026-02-30"}`},
		"date format":          {"edit_due", `{"assignment":"a","due":"31.12.2026"}`},
		"unknown enum":         {"find_lessons", `{"kind":"exam"}`},
		"long label":           {"generate_draft", `{"assignment":"a","instruction":"` + strings.Repeat("a", MaxLabelRunes+1) + `"}`},
		"blank label":          {"generate_draft", `{"assignment":"a","instruction":"  "}`},
		"label newline":        {"generate_draft", `{"assignment":"a","instruction":"a\nb"}`},
		"long query":           {"search", `{"query":"` + strings.Repeat("a", MaxQueryRunes+1) + `"}`},
		"blank query":          {"search", `{"query":"  "}`},
		"long text":            {"reply", `{"text":"` + long + `"}`},
		"empty text":           {"reply", `{"text":""}`},
		"text control":         {"reply", `{"text":"a\u0007"}`},
		"long list item":       {"cannot_do", `{"your_part":["` + long + `"],"our_part":[]}`},
		"too many list items":  {"cannot_do", `{"your_part":[` + strings.Repeat(`"a",`, 10) + `"a"],"our_part":[]}`},
		"plan UI step":         {"show_plan", `{"summary":"s","steps":[{"tool":"reply"}]}`},
		"plan unknown step":    {"show_plan", `{"summary":"s","steps":[{"tool":"send_email"}]}`},
		"plan no steps":        {"show_plan", `{"summary":"s","steps":[]}`},
		"plan extra field":     {"show_plan", `{"summary":"s","steps":[{"tool":"list_subjects","to":"x"}]}`},
		"plan long argument":   {"show_plan", `{"summary":"s","steps":[{"tool":"search","arguments":{"query":"` + strings.Repeat("a", 81) + `"}}]}`},
		"plan object arg":      {"show_plan", `{"summary":"s","steps":[{"tool":"search","arguments":{"query":{}}}]}`},
		"plan bad arg name":    {"show_plan", `{"summary":"s","steps":[{"tool":"search","arguments":{"Query":"a"}}]}`},
		"question kind":        {"ask_questions", `{"questions":[{"kind":"essay","question":"q"}]}`},
		"choice one option":    {"ask_questions", `{"questions":[{"kind":"choice","question":"q","options":["a"]}]}`},
		"default out of range": {"ask_questions", `{"questions":[{"kind":"choice","question":"q","options":["a","b"],"default":2}]}`},
		"default fraction":     {"ask_questions", `{"questions":[{"kind":"choice","question":"q","options":["a","b"],"default":0.5}]}`},
		"default no options":   {"ask_questions", `{"questions":[{"kind":"text","question":"q","default":0}]}`},
		"question extra":       {"ask_questions", `{"questions":[{"kind":"text","question":"q","url":"x"}]}`},
		"no questions":         {"ask_questions", `{"questions":[]}`},
		"six questions":        {"ask_questions", `{"questions":[` + strings.Repeat(`{"kind":"text","question":"q"},`, 5) + `{"kind":"text","question":"q"}]}`},
		"long question":        {"ask_questions", `{"questions":[{"kind":"text","question":"` + long + `"}]}`},
		"null argument":        {"reply", `{"text":null}`},
	} {
		if err := c.Check(call(t, tc[0], tc[1])); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v, want ErrInvalidCall", name, err)
		} else if strings.Contains(err.Error(), "x@y") || strings.Contains(err.Error(), "я") {
			t.Errorf("%s: the error quotes the call: %v", name, err)
		}
	}
}

func TestCheckReturnedAndEcho(t *testing.T) {
	c := goldenCatalog(t)
	mk := func(id, tool, args string) values.ToolCall {
		v, err := values.NewToolCall(id, tool, args)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ok := []values.ToolCall{mk("a", "list_subjects", "{}"), mk("b", "reply", `{"text":"t"}`)}
	if err := c.CheckReturned(ok); err != nil {
		t.Fatal(err)
	}
	five := append(append([]values.ToolCall(nil), ok...), mk("c", "list_subjects", "{}"), mk("d", "list_subjects", "{}"), mk("e", "list_subjects", "{}"))
	for name, calls := range map[string][]values.ToolCall{
		"none": nil, "five": five, "repeated id": {ok[0], ok[0]}, "invalid": {ok[0], mk("c", "reply", "{}")},
	} {
		if err := c.CheckReturned(calls); !errors.Is(err, ErrInvalidCall) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := c.CheckEcho([]values.ToolCall{mk("a", "reply", "{}")}); err != nil {
		t.Fatalf("echo of a known tool: %v", err)
	}
	if err := c.CheckEcho([]values.ToolCall{mk("a", "send_email", "{}")}); !errors.Is(err, ErrInvalidCall) {
		t.Fatalf("echo of an unknown tool: err = %v", err)
	}
}

func TestParseRejectsBadCatalogs(t *testing.T) {
	raw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	base := string(raw)
	for name, text := range map[string]string{
		"not JSON":        "{",
		"unknown field":   strings.Replace(base, `"catalog_version": 3,`, `"catalog_version": 3, "extra": true,`, 1),
		"no version":      strings.Replace(base, `"catalog_version": 3,`, `"catalog_version": 0,`, 1),
		"trailing data":   base + "{}",
		"no tools":        `{"catalog_version": 3, "tools": []}`,
		"card ids on R":   strings.Replace(base, `{"name": "ids", "type": "ids"`, `{"name": "ids", "type": "card_ids"`, 1),
		"bad tool name":   strings.Replace(base, `"name": "reply"`, `"name": "Reply"`, 1),
		"duplicate tool":  strings.Replace(base, `"name": "reply"`, `"name": "cannot_do"`, 1),
		"unknown class":   strings.Replace(base, `"class": "UI"`, `"class": "X"`, 1),
		"UI type on R":    strings.Replace(base, `{"name": "query", "type": "query"`, `{"name": "query", "type": "text"`, 1),
		"action type UI":  strings.Replace(base, `{"name": "text", "type": "text"`, `{"name": "text", "type": "label"`, 1),
		"query type UI":   strings.Replace(base, `{"name": "text", "type": "text"`, `{"name": "text", "type": "query"`, 1),
		"unknown type":    strings.Replace(base, `{"name": "query", "type": "query"`, `{"name": "query", "type": "url"`, 1),
		"enum no values":  strings.Replace(base, `"values": ["subject", "all"]`, `"values": []`, 1),
		"values on query": strings.Replace(base, `{"name": "query", "type": "query", "required": true}`, `{"name": "query", "type": "query", "required": true, "values": ["a"]}`, 1),
		"repeated value":  strings.Replace(base, `"values": ["subject", "all"]`, `"values": ["all", "all"]`, 1),
		"repeated arg":    strings.Replace(base, `{"name": "to", "type": "date"`, `{"name": "from", "type": "date"`, 1),
	} {
		if text == base {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if _, err := Parse([]byte(text)); !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("%s: err = %v, want ErrInvalidCatalog", name, err)
		}
	}
}
