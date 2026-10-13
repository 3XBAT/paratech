//go:build !windows

package whispercli

import (
	"os"
	"testing"
)

// makeDirLink creates a symlink to the directory target.
func makeDirLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}
