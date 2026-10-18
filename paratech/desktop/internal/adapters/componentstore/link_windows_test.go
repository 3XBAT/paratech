//go:build windows

package componentstore

import (
	"os/exec"
	"testing"
)

// makeDirLink creates a junction: unlike a symlink it needs no privilege on Windows.
func makeDirLink(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v: %s", err, out)
	}
}
