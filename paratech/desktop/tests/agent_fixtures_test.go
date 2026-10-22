package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Agent eval fixtures shared with the Hub tests: shared/testdata/agent (README.md there describes them).

// replayCall is one tool call of a recorded AgentStep answer, as the Hub returned it.
type replayCall struct {
	CallID        string `json:"call_id"`
	Tool          string `json:"tool"`
	ArgumentsJSON string `json:"arguments_json"`
}

// replayFixture is a recorded planner step and what each side must do with it.
type replayFixture struct {
	Name        string            `json:"name"`
	About       string            `json:"about"`
	Calls       []replayCall      `json:"calls"`
	Hub         string            `json:"hub"`          // accept | reject
	Desktop     map[string]string `json:"desktop"`      // call id -> driver:<class> | card:<kind> | refused:<refusal>
	DesktopStop string            `json:"desktop_stop"` // the stop reason when the whole reply stops the request
}

// injectionFixture is untrusted text and the property it must never break.
type injectionFixture struct {
	Name          string `json:"name"`
	Source        string `json:"source"`
	Untrusted     string `json:"untrusted"`
	Property      string `json:"property"`
	RefusedAsName bool   `json:"refused_as_name"`
	PendingDriver string `json:"pending_driver"`
}

func agentFixtureFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(codeRoot(t), "shared", "testdata", "agent", dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures in shared/testdata/agent/%s (%v)", dir, err)
	}
	slices.Sort(files)
	return files
}

func decodeFixture(t *testing.T, file string, v any) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", filepath.Base(file), err)
	}
}

func loadReplayFixtures(t *testing.T) []replayFixture {
	t.Helper()
	var out []replayFixture
	for _, f := range agentFixtureFiles(t, "replay") {
		var fx replayFixture
		decodeFixture(t, f, &fx)
		if fx.Name+".json" != filepath.Base(f) || len(fx.Calls) == 0 || (fx.Hub != "accept" && fx.Hub != "reject") ||
			(len(fx.Desktop) == 0) == (fx.DesktopStop == "") {
			t.Fatalf("%s: malformed replay fixture", filepath.Base(f))
		}
		out = append(out, fx)
	}
	return out
}

func loadInjectionFixtures(t *testing.T) []injectionFixture {
	t.Helper()
	var out []injectionFixture
	for _, f := range agentFixtureFiles(t, "injection") {
		var fx injectionFixture
		decodeFixture(t, f, &fx)
		if fx.Name+".json" != filepath.Base(f) || fx.Untrusted == "" || fx.Property == "" || fx.Source == "" {
			t.Fatalf("%s: malformed injection fixture", filepath.Base(f))
		}
		out = append(out, fx)
	}
	return out
}
