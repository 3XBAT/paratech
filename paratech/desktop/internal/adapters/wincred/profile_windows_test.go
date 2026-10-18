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

// Names that only fold to the default dir's name are other directories on NTFS and must get their own profile
// (strings.ToLower maps U+0130 to 'i' and U+212A to 'k'). Other spellings of the default dir itself stay default.
func TestProfileForUsesFileIdentity(t *testing.T) {
	cases := []struct {
		desc        string
		defName     string
		dataName    string // created unless it is a spelling of defName
		create      bool
		wantDefault bool
	}{
		{"dotted capital I", "LearningAssistant", "Learn\u0130ngAssistant", true, false},
		{"Kelvin sign", "LearningkA", "Learning\u212AA", true, false},
		{"Angstrom sign", "Learning\u00e5", "Learning\u212B", true, false},
		{"upper case", "LearningAssistant", "LEARNINGASSISTANT", false, true},
		{"trailing separator", "LearningAssistant", `LearningAssistant\`, false, true},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			base := t.TempDir()
			def := filepath.Join(base, c.defName)
			if err := os.Mkdir(def, 0o700); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(base, c.dataName)
			if c.create {
				if err := os.Mkdir(data, 0o700); err != nil {
					t.Skipf("the file system refuses %q next to %q: %v", c.dataName, c.defName, err)
				}
			}

			got, back := ProfileFor(data, def), ProfileFor(def, data)
			if c.wantDefault {
				if got != DefaultProfile || back != DefaultProfile {
					t.Fatalf("ProfileFor = %q, %q; want the default profile", got, back)
				}
				return
			}
			if !got.Isolated() || !back.Isolated() {
				t.Fatalf("ProfileFor = %q, %q; want isolated profiles", got, back)
			}
			if got == back {
				t.Fatalf("two directories share the profile %q", got)
			}
		})
	}
}
