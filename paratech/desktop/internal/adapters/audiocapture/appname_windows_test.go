//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"os"
	"path/filepath"
	"testing"
)

// The version resource of a system executable is read (silent: nothing is started).
func TestDescribeSystemExecutable(t *testing.T) {
	w, ok := platformApps().(*winApps)
	if !ok {
		t.Fatal("platformApps is not the Windows implementation")
	}
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no %s: %v", path, err)
	}
	desc, product := w.describe(path)
	if desc == "" && product == "" {
		t.Fatalf("describe(%s) found no file description or product name", path)
	}
	t.Logf("notepad.exe: description %q, product %q", desc, product)

	if d, p := w.describe(filepath.Join(t.TempDir(), "missing.exe")); d != "" || p != "" {
		t.Fatalf("a missing file described as %q, %q", d, p)
	}
}
