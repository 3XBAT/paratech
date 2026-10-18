//go:build windows

package wincred

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestStripFinalPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\\?\C:\Users\x\AppData\Local\LearningAssistant`, `C:\Users\x\AppData\Local\LearningAssistant`},
		{`\\?\UNC\server\share\dir`, `\\server\share\dir`},
		{`C:\plain`, `C:\plain`},
	}
	for _, c := range cases {
		if got := stripFinalPrefix(c.in); got != c.want {
			t.Errorf("stripFinalPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Windows paths are case-insensitive: another case of the default dir is the default dir.
func TestProfileForIgnoresCase(t *testing.T) {
	def := filepath.Join(t.TempDir(), "LearningAssistant")
	if err := os.Mkdir(def, 0o700); err != nil {
		t.Fatal(err)
	}

	for _, spelling := range []string{strings.ToUpper(def), strings.ToLower(def), strings.ToLower(def) + `\`} {
		if got := ProfileFor(spelling, def); got != DefaultProfile {
			t.Errorf("ProfileFor(%q) = %q, want the default profile", spelling, got)
		}
		if got := ProfileFor(def, spelling); got != DefaultProfile {
			t.Errorf("ProfileFor with default %q = %q, want the default profile", spelling, got)
		}
	}
}

// A junction (mklink /J needs no privilege) to the default dir is the default dir; EvalSymlinks would miss it.
func TestProfileForFollowsJunction(t *testing.T) {
	base := t.TempDir()
	def := filepath.Join(base, "real")
	if err := os.Mkdir(def, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, def).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v: %s", err, out)
	}

	if got := ProfileFor(link, def); got != DefaultProfile {
		t.Fatalf("ProfileFor(junction, default) = %q, want the default profile", got)
	}
	if got := ProfileFor(def, link); got != DefaultProfile {
		t.Fatalf("ProfileFor(default, junction) = %q, want the default profile", got)
	}
}

// The 8.3 short name of the default dir is the default dir.
func TestProfileForExpandsShortName(t *testing.T) {
	def := filepath.Join(t.TempDir(), "LearningAssistantLongName")
	if err := os.Mkdir(def, 0o700); err != nil {
		t.Fatal(err)
	}
	short := shortPath(t, def)
	if strings.EqualFold(filepath.Base(short), filepath.Base(def)) {
		t.Skipf("the volume makes no 8.3 names (%s)", short)
	}

	if got := ProfileFor(short, def); got != DefaultProfile {
		t.Fatalf("ProfileFor(%q, default) = %q, want the default profile", short, got)
	}
	if got := ProfileFor(def, short); got != DefaultProfile {
		t.Fatalf("ProfileFor(default, %q) = %q, want the default profile", short, got)
	}
}

func shortPath(t *testing.T, path string) string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || int(n) >= len(buf) {
		t.Skipf("GetShortPathName: %v (n=%d)", err, n)
	}
	return windows.UTF16ToString(buf[:n])
}
