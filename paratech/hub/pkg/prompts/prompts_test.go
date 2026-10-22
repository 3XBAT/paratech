package prompts

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

// goldenCatalog is the contract's golden catalog v1 (TASK-113); the embedded copy must stay byte-identical.
const goldenCatalog = "../../../shared/testdata/tool_catalog_v1.json"

func TestEmbeddedPanelV1(t *testing.T) {
	versions, err := Agent("panel")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("versions = %+v, want only v1", versions)
	}
	v1 := versions[0]
	golden, err := os.ReadFile(goldenCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(v1.Tools, golden) {
		t.Fatal("agent/panel/v1/tools.json differs from shared/testdata/tool_catalog_v1.json")
	}
	if len(bytes.TrimSpace(v1.System)) == 0 || !utf8.Valid(v1.System) || bytes.IndexByte(v1.System, 0) >= 0 {
		t.Fatal("agent/panel/v1/system.md must be non-empty UTF-8 without NUL")
	}
}

func TestNoEmbeddedOperationPromptsYet(t *testing.T) {
	for _, op := range []string{"inspect_material", "answer", "triage_assignment"} {
		if _, _, err := Op(op); !errors.Is(err, ErrNotFound) {
			t.Errorf("Op(%s): err = %v, want ErrNotFound", op, err)
		}
	}
}

func TestVersionDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"agent/p/v2/system.md":    {Data: []byte("two")},
		"agent/p/v2/tools.json":   {Data: []byte("{}")},
		"agent/p/v10/system.md":   {Data: []byte("ten")},
		"agent/p/v10/tools.json":  {Data: []byte("{}")},
		"ops/answer/v1/system.md": {Data: []byte("old")},
		"ops/answer/v3/system.md": {Data: []byte("new")},
		"agent/half/v1/system.md": {Data: []byte("no tools")},
		"agent/odd/v01/system.md": {Data: []byte("x")},
		"agent/file/notes.txt":    {Data: []byte("x")},
	}
	got, err := agentIn(fsys, "p")
	if err != nil || len(got) != 2 || got[0].Version != 2 || got[1].Version != 10 || string(got[1].System) != "ten" {
		t.Fatalf("agentIn = %+v, %v", got, err)
	}
	v, system, err := opIn(fsys, "answer")
	if err != nil || v != 3 || string(system) != "new" {
		t.Fatalf("opIn = %d %q %v, want the newest", v, system, err)
	}
	for _, name := range []string{"half", "odd", "file"} {
		if _, err := agentIn(fsys, name); err == nil {
			t.Errorf("agentIn(%s): no error", name)
		}
	}
	for _, name := range []string{"missing", ""} {
		if _, err := agentIn(fsys, name); err == nil {
			t.Errorf("agentIn(%q): no error", name)
		}
	}
	if _, err := agentIn(fsys, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing profile: err = %v, want ErrNotFound", err)
	}
	for _, name := range []string{"../agent", "A", "p/v2"} {
		if _, _, err := opIn(fsys, name); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("opIn(%q): err = %v, want a name error", name, err)
		}
	}
}
