package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
)

// goldenCatalog is shared/testdata/tool_catalog_v1.json (TASK-113), the tool list the Hub serves.
type goldenCatalog struct {
	CatalogVersion int               `json:"catalog_version"`
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
	raw, err := os.ReadFile(filepath.Join(codeRoot(t), "shared", "testdata", "tool_catalog_v1.json"))
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

// TestOneOfPairsMatchGolden checks the one-of pairs the golden notes give to the desktop parser (TASK-113).
func TestOneOfPairsMatchGolden(t *testing.T) {
	golden := loadGoldenCatalog(t)
	note := golden.Notes["one_of"]
	matches := regexp.MustCompile(`(\w+) \((\w+)\|(\w+)\)`).FindAllStringSubmatch(note, -1)
	if len(matches) == 0 {
		t.Fatalf("no pairs in the one_of note %q", note)
	}

	want := map[values.Tool][2]string{}
	for _, m := range matches {
		want[values.Tool(m[1])] = [2]string{m[2], m[3]}
	}
	for _, tool := range values.Tools() {
		spec, err := values.SpecOf(tool)
		if err != nil {
			t.Fatal(err)
		}
		got := spec.OneOf()
		pair, ok := want[tool]
		switch {
		case !ok && len(got) != 0:
			t.Errorf("%s has one-of pairs %v not in the golden note", tool, got)
		case ok && (len(got) != 1 || got[0] != pair):
			t.Errorf("%s one-of = %v, golden %v", tool, got, pair)
		}
	}
}
