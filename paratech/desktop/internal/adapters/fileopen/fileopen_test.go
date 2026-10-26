package fileopen

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// newRoot returns an artifact root with the file exports/library/m1/<name> for each name.
func newRoot(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "exports", "library", "m1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func ref(t *testing.T, name string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(shared.ArtifactExports, "library/m1/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// noRun fails the test when anything would be launched.
func noRun(t *testing.T) runner {
	return func(context.Context, string, string, string) error {
		t.Error("the launcher was called")
		return nil
	}
}

func TestResolve(t *testing.T) {
	root := newRoot(t, "Реферат.docx", "slides.PPTX", "notes.md", "a.txt", "scan.pdf", "tool.exe", "noext", "a,b.docx")
	if err := os.Mkdir(filepath.Join(root, "exports", "library", "m1", "dir.docx"), 0o700); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		want error // nil: resolved
	}{
		{name: "Реферат.docx"},
		{name: "slides.PPTX"},
		{name: "notes.md"},
		{name: "a.txt"},
		{name: "scan.pdf"},
		{name: "tool.exe", want: shared.ErrInvalidArtifactRef},
		{name: "noext", want: shared.ErrInvalidArtifactRef},
		{name: "a,b.docx", want: shared.ErrInvalidArtifactRef},
		{name: "dir.docx", want: shared.ErrInvalidArtifactRef},
		{name: "missing.docx", want: shared.ErrNotFound},
	}
	o := &Opener{root: root, run: noRun(t)}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := o.resolve(ref(t, tt.name))
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("resolve = %q, %v; want %v", got, err, tt.want)
				}
				return
			}
			want := filepath.Join(realRoot, "exports", "library", "m1", tt.name)
			if err != nil || got != want {
				t.Fatalf("resolve = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestResolveRefusesBadInput(t *testing.T) {
	root := newRoot(t, "a.docx")
	for name, o := range map[string]*Opener{
		"no root":       {run: noRun(t)},
		"relative root": {root: "artifacts", run: noRun(t)},
	} {
		if _, err := o.resolve(ref(t, "a.docx")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
			t.Errorf("%s: resolve = %v, want refused", name, err)
		}
	}
	if _, err := (&Opener{root: root}).resolve(shared.ArtifactRef{}); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("zero reference: resolve = %v, want refused", err)
	}
	if _, err := (&Opener{root: filepath.Join(root, "gone")}).resolve(ref(t, "a.docx")); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("missing root: resolve = %v, want not found", err)
	}
}

// A link inside the root that points outside it is refused after the links are resolved.
func TestResolveRefusesALinkOutOfTheRoot(t *testing.T) {
	root := newRoot(t)
	outside := filepath.Join(t.TempDir(), "secret.docx")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "exports", "library", "m1", "link.docx")); err != nil {
		t.Skipf("symbolic links are not available here: %v", err)
	}
	o := &Opener{root: root, run: noRun(t)}
	if _, err := o.resolve(ref(t, "link.docx")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("resolve = %v, want refused", err)
	}
	if err := o.OpenArtifact(context.Background(), ref(t, "link.docx")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("OpenArtifact = %v, want refused", err)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifacts")
	tests := []struct {
		path string
		want bool
	}{
		{path: filepath.Join(root, "exports", "a.docx"), want: true},
		{path: filepath.Join(root, "..x"), want: true},
		{path: root},
		{path: filepath.Dir(root)},
		{path: root + "x"},
		{path: filepath.Join(root, "..", "other", "a.docx")},
		{path: "relative.docx"},
	}
	for _, tt := range tests {
		if got := within(root, tt.path); got != tt.want {
			t.Errorf("within(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestOpenArtifactChecksBeforeTheStart(t *testing.T) {
	root := newRoot(t, "tool.exe")
	o := &Opener{root: root, run: noRun(t)}
	if err := o.OpenArtifact(context.Background(), ref(t, "tool.exe")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("OpenArtifact(tool.exe) = %v, want refused", err)
	}
	if err := o.OpenArtifact(context.Background(), ref(t, "missing.docx")); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("OpenArtifact(missing) = %v, want not found", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.OpenArtifact(ctx, ref(t, "tool.exe")); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenArtifact(cancelled) = %v, want context.Canceled", err)
	}
}

// No error of the check names the path.
func TestErrorsNameNoPath(t *testing.T) {
	root := newRoot(t, "tool.exe")
	o := &Opener{root: root, run: noRun(t)}
	for _, name := range []string{"tool.exe", "missing.docx"} {
		err := o.OpenArtifact(context.Background(), ref(t, name))
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		for _, part := range []string{root, name} {
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(part)) {
				t.Errorf("%s: error %q names %q", name, err, part)
			}
		}
	}
}
