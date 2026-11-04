//go:build windows

package e2ehooks

import (
	"io/fs"
	"os"
	"os/exec"
	"testing"
)

// junction makes a real NTFS junction (mount point) with mklink /J, the likely attack: it needs no privilege.
func junction(t *testing.T, target, link string) {
	t.Helper()
	out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("mklink /J refused: %v: %s", err, out)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode().Type() == fs.ModeDir {
		t.Fatalf("the junction reads as a plain folder (%v): the check would not see it", err)
	}
}

func TestPickRefusesJunctions(t *testing.T) { linkCases(t, junction) }

func TestSaveRefusesJunctions(t *testing.T) { saveLinkCases(t, junction) }
