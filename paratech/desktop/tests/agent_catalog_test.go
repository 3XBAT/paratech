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

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/testenv"
)

// goldenCatalog is shared/testdata/tool_catalog_v4.json (TASK-113; v2 since TASK-118, v3 since TASK-173, v4 since
// TASK-184), the tool list the Hub serves for the catalog version the desktop sends.
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
	raw, err := os.ReadFile(filepath.Join(codeRoot(t), "shared", "testdata", "tool_catalog_v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c goldenCatalog
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
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
