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

// fakeProcess has nothing to tell off Windows: the load modes only set threads here.
func fakeProcess() []string { return nil }

func TestProcGroupIgnoresTheCap(t *testing.T) {
	g, err := newProcGroup(40)
	if g == nil || err != nil {
		t.Fatalf("newProcGroup(40) = %v, %v; want a no-op group without an error", g, err)
	}
	g.close()
}
