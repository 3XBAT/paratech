package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/testenv"
)

// goldenCatalog is shared/testdata/tool_catalog_v8.json (TASK-113; v2 since TASK-118, v3 since TASK-173, v4 since
// TASK-184, v5 since TASK-210, v6 since TASK-217, v7 since TASK-223, v8 since TASK-234), the tool list the Hub serves for the catalog
// version the desktop sends.
type goldenCatalog struct {
	CatalogVersion int               `json:"catalog_version"`
	Types          map[string]string `json:"types"`
	Notes          map[string]string `json:"notes"`
	Tools          []struct {
		Name            string `json:"name"`
		Class           string `json:"class"`
		Paid            bool   `json:"paid"`
		AgentCanExecute bool   `json:"agent_can_execute"`
		Arguments       []struct {
			Name     string   `json:"name"`
			Type     string   `json:"type"`
			Required bool     `json:"required"`
			Values   []string `json:"values"`
		} `json:"arguments"`
	} `json:"tools"`
}

func loadGoldenCatalog(t *testing.T) goldenCatalog {
	t.Helper()
	return loadGoldenCatalogFile(t, fmt.Sprintf("tool_catalog_v%d.json", values.CatalogVersion))
}

func loadGoldenCatalogFile(t *testing.T, name string) goldenCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(codeRoot(t), "shared", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var c goldenCatalog
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestToolCatalogV5KeepsTheToolsOfV4: v5 changed only the Hub panel prompt (TASK-200), so a desktop on v5 runs the
// same tools, arguments and policy as on v4: the files differ only in catalog_version.
func TestToolCatalogV5KeepsTheToolsOfV4(t *testing.T) {
	dir := filepath.Join(codeRoot(t), "shared", "testdata")
	v4, err := os.ReadFile(filepath.Join(dir, "tool_catalog_v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	v5, err := os.ReadFile(filepath.Join(dir, "tool_catalog_v5.json"))
	if err != nil {
		t.Fatal(err)
	}

	const v4Line, v5Line = `"catalog_version": 4,`, `"catalog_version": 5,`
	if strings.Count(string(v4), v4Line) != 1 || strings.Count(string(v5), v5Line) != 1 {
		t.Fatal("catalog_version lines changed")
	}
	if strings.Replace(string(v4), v4Line, v5Line, 1) != string(v5) {
		t.Fatal("tool_catalog_v5.json differs from tool_catalog_v4.json beyond catalog_version")
	}
}

// TestToolCatalogV6AddsCreateAssignment: v6 is v5 plus create_assignment (class U, free) and the argument type time
// (TASK-216); every tool of v5 keeps its class and arguments, so the desktop runs them as on v5.
func TestToolCatalogV6AddsCreateAssignment(t *testing.T) {
	v5, v6 := loadGoldenCatalogFile(t, "tool_catalog_v5.json"), loadGoldenCatalogFile(t, "tool_catalog_v6.json")
	// The desktop refuses a time without its due date (is_error); the Hub only states the rule.
	if v6.CatalogVersion != 6 || !strings.Contains(v6.Types["time"], "HH:MM") || !strings.Contains(v6.Types["time"], "only together with due") ||
		v5.Types["time"] != "" || len(v6.Types) != len(v5.Types)+1 {
		t.Fatalf("v6 types = %v", v6.Types)
	}
	var rest []string
	for _, tool := range v6.Tools {
		if tool.Name == string(values.ToolCreateAssignment) {
			if tool.Class != "U" || tool.Paid || !tool.AgentCanExecute {
				t.Fatalf("create_assignment = %+v", tool)
			}
			continue
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		rest = append(rest, string(raw))
	}
	var old []string
	for _, tool := range v5.Tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		old = append(old, string(raw))
	}
	if !slices.Equal(rest, old) {
		t.Fatal("a tool of v5 changed in v6")
	}
	if v6.Notes["one_of"] != v5.Notes["one_of"] {
		t.Fatal("the one-of groups changed in v6")
	}
}

// TestToolCatalogV7HasALongReplyText: v7 is v6 with reply.text of the new type reply_text (TASK-223); every other
// tool, argument, type and note stays as in v6, so only the reply card's text grows.
func TestToolCatalogV7HasALongReplyText(t *testing.T) {
	v6, v7 := loadGoldenCatalogFile(t, "tool_catalog_v6.json"), loadGoldenCatalogFile(t, "tool_catalog_v7.json")
	note, text := v7.Types["reply_text"], v7.Types["text"]
	if v7.CatalogVersion != 7 || v6.Types["reply_text"] != "" || !strings.Contains(note, "without HTML") ||
		!strings.Contains(note, fmt.Sprintf("at most %d characters", values.MaxReplyTextRunes)) ||
		!strings.Contains(text, fmt.Sprintf("at most %d characters", values.MaxTextRunes)) {
		t.Fatalf("v7 types = %v", v7.Types)
	}

	// v7 with reply.text back to text and without the reply_text type is v6 apart from the version.
	want := v7
	want.CatalogVersion = 6
	want.Tools = slices.Clone(v7.Tools)
	want.Types = map[string]string{}
	for k, v := range v7.Types {
		if k != "reply_text" {
			want.Types[k] = v
		}
	}
	replies := 0
	for i, tool := range want.Tools {
		for j, a := range tool.Arguments {
			if a.Type != "reply_text" {
				continue
			}
			if tool.Name != string(values.ToolReply) || a.Name != "text" {
				t.Fatalf("%s.%s is a reply_text", tool.Name, a.Name)
			}
			replies++
			args := slices.Clone(tool.Arguments)
			args[j].Type = "text"
			want.Tools[i].Arguments = args
		}
	}
	if replies != 1 {
		t.Fatalf("%d reply_text arguments, want reply.text only", replies)
	}
	got, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	old, err := json.Marshal(v6)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(old) {
		t.Fatal("tool_catalog_v7.json differs from tool_catalog_v6.json beyond reply.text")
	}
}

// TestToolCatalogV8KeepsTheToolsOfV7: v8 changed only the Hub panel prompt (it asks the subject of an assignment,
// TASK-233), so a desktop on v8 runs the same tools, arguments and policy as on v7: the files differ only in
// catalog_version.
func TestToolCatalogV8KeepsTheToolsOfV7(t *testing.T) {
	dir := filepath.Join(codeRoot(t), "shared", "testdata")
	v7, err := os.ReadFile(filepath.Join(dir, "tool_catalog_v7.json"))
	if err != nil {
		t.Fatal(err)
	}
	v8, err := os.ReadFile(filepath.Join(dir, "tool_catalog_v8.json"))
	if err != nil {
		t.Fatal(err)
	}

	const v7Line, v8Line = `"catalog_version": 7,`, `"catalog_version": 8,`
	if strings.Count(string(v7), v7Line) != 1 || strings.Count(string(v8), v8Line) != 1 {
		t.Fatal("catalog_version lines changed")
	}
	if strings.Replace(string(v7), v7Line, v8Line, 1) != string(v8) {
		t.Fatal("tool_catalog_v8.json differs from tool_catalog_v7.json beyond catalog_version")
	}
}

// TestPanelPromptV8PinsTheSubjectQuestion: the desktop recognizes the subject question by its exact text and last
// option (TASK-234), so the Hub panel prompt v8 must spell both the same way, or nothing preselected silently breaks.
func TestPanelPromptV8PinsTheSubjectQuestion(t *testing.T) {
	prompt, err := os.ReadFile(filepath.Join(codeRoot(t), "hub", "pkg", "prompts", "agent", "panel", "v8", "system.md"))
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{values.SubjectQuestion, values.NoSubjectOption} {
		if !strings.Contains(string(prompt), "«"+s+"»") {
			t.Errorf("panel prompt v8 does not contain «%s»", s)
		}
	}
}

// A long reply of catalog v7 (TASK-223) goes through the Hub client, the thread and SQLite whole, in paragraphs and
// with angle brackets as text; after a restart the next request sends it to the Hub as the 2 KiB history reply only.
func TestAgentLongReplyOfCatalog7(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	paragraph := strings.Repeat("Длинный ответ по теме. ", 40)
	text := strings.Join([]string{paragraph, paragraph, paragraph, paragraph}, "\n\n") + "Итог: a < b, b > c."
	if n := utf8.RuneCountInString(text); n <= values.MaxTextRunes*10 || n > values.MaxReplyTextRunes {
		t.Fatalf("text of %d runes", n)
	}
	e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "reply", Args: `{"text":` + mustQuote(t, text) + `}`})
	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "расскажи подробно"})
	if err != nil || st.Status != thread.StatusDone || len(st.Cards) != 1 {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	if card, ok := st.Cards[0].(values.ReplyCard); !ok || string(card.Text) != text || card.Validate() != nil {
		t.Fatalf("card = %+v", st.Cards[0])
	}

	e.start(t)
	e.hub.Then(testenv.AgentCall{ID: "toolu_2", Tool: "reply", Args: `{"text":"Короче: a < b."}`})
	if st, err = e.turns.Ask(ctx, agentturn.AskInput{Text: "а короче?"}); err != nil || st.Status != thread.StatusDone || st.Turn != 2 {
		t.Fatalf("second Ask = %+v, %v", st, err)
	}
	reqs := e.hub.Requests()
	var replies []string
	for _, turn := range reqs[len(reqs)-1].GetTurns() {
		if r := turn.GetAgentReply(); r != "" {
			replies = append(replies, r)
		}
	}
	if len(replies) != 1 || len(replies[0]) > values.MaxAgentReplyBytes || !utf8.ValidString(replies[0]) ||
		!strings.HasPrefix(replies[0], "reply: Длинный ответ по теме.") || strings.Contains(replies[0], "Итог") {
		t.Fatalf("history replies = %q", replies)
	}
}

// TestToolCatalogMatchesGolden keeps the desktop's closed tool enum, argument schemas and policy classes equal to the
// golden catalog: a tool added on one side only fails here.
func TestToolCatalogMatchesGolden(t *testing.T) {
	golden := loadGoldenCatalog(t)
	if golden.CatalogVersion != values.CatalogVersion {
		t.Fatalf("catalog_version = %d, desktop implements %d", golden.CatalogVersion, values.CatalogVersion)
	}

	var names []values.Tool
	for _, g := range golden.Tools {
		names = append(names, values.Tool(g.Name))
	}
	if !slices.Equal(names, values.Tools()) {
		t.Fatalf("tool enum differs from the golden list:\n desktop %v\n golden  %v", values.Tools(), names)
	}

	for _, g := range golden.Tools {
		t.Run(g.Name, func(t *testing.T) {
			tool := values.Tool(g.Name)
			rule, err := actionpolicy.Of(tool)
			if err != nil {
				t.Fatal(err)
			}
			if string(rule.Class) != g.Class || rule.Paid != g.Paid || rule.Class.AgentCanExecute() != g.AgentCanExecute {
				t.Fatalf("policy %+v (execute %v), golden class %s paid %v execute %v",
					rule, rule.Class.AgentCanExecute(), g.Class, g.Paid, g.AgentCanExecute)
			}
			if _, undoable := action.CompensationFor(tool); undoable != (g.Class == "U") {
				t.Fatalf("compensation %v for class %s", undoable, g.Class)
			}

			spec, err := values.SpecOf(tool)
			if err != nil {
				t.Fatal(err)
			}
			args := spec.Args()
			if len(args) != len(g.Arguments) {
				t.Fatalf("%d arguments, golden %d", len(args), len(g.Arguments))
			}
			for i, ga := range g.Arguments {
				a := args[i]
				if a.Name != ga.Name || string(a.Type) != ga.Type || a.Required != ga.Required || !slices.Equal(a.Values, ga.Values) {
					t.Fatalf("argument %d = %+v, golden %+v", i, a, ga)
				}
			}
		})
	}
}

// TestArgumentTypesMatchGolden: every argument type of the desktop parser is a golden type, and card_ids keeps the
// bounds the golden states (0-10 IDs, TASK-173) and names an ID kind, so its IDs pass the provenance check.
func TestArgumentTypesMatchGolden(t *testing.T) {
	golden := loadGoldenCatalog(t)
	for _, tool := range values.Tools() {
		spec, err := values.SpecOf(tool)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range spec.Args() {
			if _, ok := golden.Types[string(a.Type)]; !ok {
				t.Errorf("%s.%s has type %q, not a golden type", tool, a.Name, a.Type)
			}
			if a.Type == values.ArgCardIDs && !a.Kind.Valid() {
				t.Errorf("%s.%s: card_ids without an ID kind", tool, a.Name)
			}
		}
	}
	if note := golden.Types["card_ids"]; !strings.Contains(note, fmt.Sprintf("0-%d IDs", values.MaxCardIDs)) {
		t.Fatalf("card_ids note %q does not say 0-%d IDs", note, values.MaxCardIDs)
	}
	reply, err := values.SpecOf(values.ToolReply)
	if err != nil {
		t.Fatal(err)
	}
	if args := reply.Args(); len(args) != 2 || args[1].Kind != values.KindAssignment {
		t.Fatalf("reply arguments = %+v, want assignments of kind assignment", args)
	}
}

// oneOfGroup is a tool and its one-of group in the golden note: "tool (a|b)" or "tool (a|b|c)".
var oneOfGroup = regexp.MustCompile(`(\w+) \((\w+(?:\|\w+)+)\)`)

// TestOneOfGroupsMatchGolden checks the one-of groups the golden notes give to the desktop parser (TASK-113; groups
// of three since catalog v4, TASK-184).
func TestOneOfGroupsMatchGolden(t *testing.T) {
	golden := loadGoldenCatalog(t)
	note := golden.Notes["one_of"]
	matches := oneOfGroup.FindAllStringSubmatch(note, -1)
	if len(matches) == 0 {
		t.Fatalf("no groups in the one_of note %q", note)
	}

	want := map[values.Tool][]string{}
	for _, m := range matches {
		want[values.Tool(m[1])] = strings.Split(m[2], "|")
	}
	if g := want[values.ToolTriageAssignment]; !slices.Equal(g, []string{"assignment", "upload", "topic"}) {
		t.Fatalf("golden triage_assignment group = %v", g)
	}
	for _, tool := range values.Tools() {
		spec, err := values.SpecOf(tool)
		if err != nil {
			t.Fatal(err)
		}
		got := spec.OneOf()
		group, ok := want[tool]
		switch {
		case !ok && len(got) != 0:
			t.Errorf("%s has one-of groups %v not in the golden note", tool, got)
		case ok && (len(got) != 1 || !slices.Equal(got[0], group)):
			t.Errorf("%s one-of = %v, golden %v", tool, got, group)
		}
	}
	for tool := range want {
		if !tool.Valid() || slices.Contains(values.RetiredTools(), tool) {
			t.Errorf("the one_of note names %s, not a catalog tool", tool)
		}
	}
}

// TestRetiredToolsKeepTheirStoredData: a tool that left the catalog (generate_draft in v4) is not in the golden list
// and the planner's call is an unknown tool, but its policy class and Undo stay for the actions stored before.
func TestRetiredToolsKeepTheirStoredData(t *testing.T) {
	golden := loadGoldenCatalog(t)
	for _, tool := range values.RetiredTools() {
		for _, g := range golden.Tools {
			if g.Name == string(tool) {
				t.Fatalf("%s is retired but in the golden catalog", tool)
			}
		}
		if _, err := values.ParseCall("toolu_1", string(tool), `{"assignment":"a1"}`); !errors.Is(err, values.ErrUnknownTool) {
			t.Fatalf("%s: ParseCall = %v, want an unknown tool", tool, err)
		}
		if _, err := actionpolicy.Of(tool); err != nil {
			t.Fatalf("%s lost its policy: %v", tool, err)
		}
		if _, ok := action.CompensationFor(tool); !ok {
			t.Fatalf("%s lost its Undo", tool)
		}
	}
	if !slices.Contains(values.RetiredTools(), values.ToolGenerateDraft) {
		t.Fatal("generate_draft is not retired")
	}
}

// A request started on an older catalog and stopped keeps calls sealed for that catalog. After the update the Hub
// refuses their echo (the seal binds the catalog version): «Продолжить» and «Повторить» stop with «не удалось»
// instead of hanging, and a new request in the same thread works, as it sends earlier requests as texts only
// (TASK-183). The fake Hub refuses a seal that does not match like the real one.
func TestAgentRequestOfAnOlderCatalogAfterTheUpdate(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()
	e.hub.Then(testenv.AgentCall{ID: "toolu_1", Tool: "list_subjects", Args: `{}`})

	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "какие предметы?"})
	if err != nil || len(st.Calls) != 1 {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	// The next step finds no Hub answer: the request stops with its calls echoed under the old seal.
	st = e.submit(t, st, agentturn.Outcome{Result: values.ToolResult{CallID: "toolu_1", ResultJSON: `{"subjects":[]}`}})
	if st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("stopped = %+v", st)
	}

	// The update: the stored seal is one of the older catalog, and the app starts again.
	if _, err := e.db.DBTX(ctx).ExecContext(ctx,
		`UPDATE agent_steps SET body = json_set(body, '$.seal', 'AQID') WHERE entry = 'calls'`); err != nil {
		t.Fatal(err)
	}
	e.start(t)

	before := len(e.hub.Requests())
	st, err = e.turns.Continue(ctx, turnOf(st))
	if err != nil || st.Status != thread.StatusStopped || st.StopReason != values.StopFailed {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	st, err = e.turns.Retry(ctx, turnOf(st))
	if err != nil || st.Status != thread.StatusStopped || st.StopReason != values.StopFailed {
		t.Fatalf("Retry = %+v, %v", st, err)
	}
	reqs := e.hub.Requests()
	if len(reqs) != before+2 || e.hub.Executed() != 1 {
		t.Fatalf("hub saw %d new requests, ran %d", len(reqs)-before, e.hub.Executed())
	}
	for _, r := range reqs[before:] {
		if r.GetCatalogVersion() != values.CatalogVersion {
			t.Fatalf("catalog version %d", r.GetCatalogVersion())
		}
	}

	e.hub.Then(testenv.AgentCall{ID: "toolu_2", Tool: "reply", Args: `{"text":"Предметов пока нет."}`})
	st, err = e.turns.Ask(ctx, agentturn.AskInput{Text: "а что сейчас?"})
	if err != nil || st.Status != thread.StatusDone || st.Turn != 2 {
		t.Fatalf("new request = %+v, %v", st, err)
	}
	for _, turn := range e.hub.Requests()[len(e.hub.Requests())-1].GetTurns() {
		if turn.GetCalls() != nil {
			t.Fatal("the new request echoed calls of the older catalog")
		}
	}
}

// The update to the current catalog (4 -> 5 in TASK-210, 5 -> 6 in TASK-217, 6 -> 7 in TASK-223, 7 -> 8 in TASK-234,
// where the tools stay the same and only the seal's catalog differs): calls sealed for a
// request stopped on the previous catalog no longer pass the Hub's check once the desktop sends the new one, so
// «Продолжить» and «Повторить» end with the honest «не удалось», and a new request works.
func TestAgentRequestOfThePreviousCatalogAfterTheUpdate(t *testing.T) {
	e := newAgentEnv(t)
	ctx := context.Background()

	// Before the update: the Hub seals and checks the calls for the previous catalog.
	e.hub.AsCatalog(values.CatalogVersion - 1).Then(testenv.AgentCall{ID: "toolu_1", Tool: "list_subjects", Args: `{}`})
	st, err := e.turns.Ask(ctx, agentturn.AskInput{Text: "какие предметы?"})
	if err != nil || len(st.Calls) != 1 {
		t.Fatalf("Ask = %+v, %v", st, err)
	}
	st = e.submit(t, st, agentturn.Outcome{Result: values.ToolResult{CallID: "toolu_1", ResultJSON: `{"subjects":[]}`}})
	if st.Status != thread.StatusStopped || st.StopReason != values.StopOffline {
		t.Fatalf("stopped = %+v", st)
	}

	// The update: the Hub binds the catalog the desktop now sends, and the app starts again.
	e.hub.AsCatalog(0)
	e.start(t)

	before := len(e.hub.Requests())
	st, err = e.turns.Continue(ctx, turnOf(st))
	if err != nil || st.Status != thread.StatusStopped || st.StopReason != values.StopFailed {
		t.Fatalf("Continue = %+v, %v", st, err)
	}
	st, err = e.turns.Retry(ctx, turnOf(st))
	if err != nil || st.Status != thread.StatusStopped || st.StopReason != values.StopFailed {
		t.Fatalf("Retry = %+v, %v", st, err)
	}
	reqs := e.hub.Requests()
	if len(reqs) != before+2 || e.hub.Executed() != 1 {
		t.Fatalf("hub saw %d new requests, ran %d", len(reqs)-before, e.hub.Executed())
	}
	for _, r := range reqs[before:] {
		if r.GetCatalogVersion() != values.CatalogVersion {
			t.Fatalf("catalog version %d", r.GetCatalogVersion())
		}
	}

	e.hub.Then(testenv.AgentCall{ID: "toolu_2", Tool: "reply", Args: `{"text":"Предметов пока нет."}`})
	st, err = e.turns.Ask(ctx, agentturn.AskInput{Text: "а что сейчас?"})
	if err != nil || st.Status != thread.StatusDone || st.Turn != 2 {
		t.Fatalf("new request = %+v, %v", st, err)
	}
}
