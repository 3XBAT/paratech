//go:build windows

package fileopen

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"paratech/desktop/internal/domain/shared"
)

// The launcher gets explorer.exe of the Windows directory and the checked path as the only argument; nothing starts.
func TestOpenArtifactStartsExplorerWithThePath(t *testing.T) {
	root := newRoot(t, "Реферат по истории.docx")
	var exe, dir, arg string
	calls := 0
	o := &Opener{root: root, run: func(_ context.Context, e, d, a string) error {
		calls++
		exe, dir, arg = e, d, a
		return nil
	}}
	if err := o.OpenArtifact(context.Background(), ref(t, "Реферат по истории.docx")); err != nil {
		t.Fatal(err)
	}

	windir, err := windows.GetWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realRoot, "exports", "library", "m1", "Реферат по истории.docx")
	if calls != 1 || exe != filepath.Join(windir, "explorer.exe") || dir != windir || arg != want {
		t.Fatalf("run(%q, %q, %q) x%d; want explorer.exe of %q and %q", exe, dir, arg, calls, windir, want)
	}
}

func TestOpenArtifactReportsALauncherError(t *testing.T) {
	root := newRoot(t, "a.docx")
	boom := errors.New("boom")
	o := &Opener{root: root, run: func(context.Context, string, string, string) error { return boom }}
	if err := o.OpenArtifact(context.Background(), ref(t, "a.docx")); !errors.Is(err, boom) {
		t.Fatalf("OpenArtifact = %v, want the launcher error", err)
	}
}

// A junction (no privilege needed, unlike a symlink) inside the root that points outside it is refused.
func TestOpenArtifactRefusesAJunctionOutOfTheRoot(t *testing.T) {
	root := newRoot(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "a.docx"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "exports", "library", "m2")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v: %s", err, out)
	}
	r, err := shared.NewArtifactRef(shared.ArtifactExports, "library/m2/a.docx")
	if err != nil {
		t.Fatal(err)
	}
	o := &Opener{root: root, run: noRun(t)}
	if err := o.OpenArtifact(context.Background(), r); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("OpenArtifact = %v, want refused", err)
	}
}

func TestLaunchRefusesAPathWithoutADriveLetter(t *testing.T) {
	o := &Opener{run: noRun(t)}
	for _, p := range []string{
		`\\server\share\a.docx`, `\\?\C:\a.docx`, `\a.docx`, `C:a.docx`, `C:/a.docx`, `1:\a.docx`, `C:\a.docx:stream`,
	} {
		if err := o.launch(context.Background(), p); !errors.Is(err, shared.ErrInvalidArtifactRef) {
			t.Errorf("launch(%q) = %v, want refused", p, err)
		}
	}
}
