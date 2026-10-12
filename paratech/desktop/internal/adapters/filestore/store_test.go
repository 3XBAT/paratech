package filestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// artifactStore is the union of the ArtifactStore ports declared in the use cases.
type artifactStore interface {
	Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error)
	Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error
	List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error)
	Size(ctx context.Context, ref shared.ArtifactRef) (int64, error)
	Delete(ctx context.Context, ref shared.ArtifactRef) error
}

var _ artifactStore = (*Store)(nil)

// newStore returns a store on <tmp>/data; the parent directory is outside the root and is checked by traversal tests.
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, root
}

func ref(t *testing.T, kind shared.ArtifactKind, p string) shared.ArtifactRef {
	t.Helper()
	r, err := shared.NewArtifactRef(kind, p)
	if err != nil {
		t.Fatalf("NewArtifactRef(%q, %q): %v", kind, p, err)
	}
	return r
}

func put(t *testing.T, s *Store, r shared.ArtifactRef, content string) {
	t.Helper()
	if err := s.Write(context.Background(), r, strings.NewReader(content)); err != nil {
		t.Fatalf("Write(%s): %v", r, err)
	}
}

func read(t *testing.T, s *Store, r shared.ArtifactRef) string {
	t.Helper()
	rc, err := s.Open(context.Background(), r)
	if err != nil {
		t.Fatalf("Open(%s): %v", r, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", r, err)
	}
	return string(b)
}

func exists(t *testing.T, s *Store, r shared.ArtifactRef) bool {
	t.Helper()
	_, err := s.Size(context.Background(), r)
	if err != nil && !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Size(%s): %v", r, err)
	}
	return err == nil
}

// tempFiles lists leftover temporary files anywhere below dir.
func tempFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), tmpPrefix) {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return out
}

func TestNew(t *testing.T) {
	t.Run("creates the layout and is idempotent", func(t *testing.T) {
		s, root := newStore(t)
		put(t, s, ref(t, shared.ArtifactModels, "m.bin"), "x")
		s2, err := New(root)
		if err != nil {
			t.Fatalf("second New: %v", err)
		}
		defer func() { _ = s2.Close() }()
		for _, k := range kinds {
			if fi, err := os.Stat(filepath.Join(root, string(k))); err != nil || !fi.IsDir() {
				t.Errorf("kind directory %s: %v", k, err)
			}
		}
		if got := read(t, s2, ref(t, shared.ArtifactModels, "m.bin")); got != "x" {
			t.Errorf("content after reopen = %q", got)
		}
	})
	t.Run("rejects relative and empty roots", func(t *testing.T) {
		for _, r := range []string{"", "data", "./data", "../data"} {
			if _, err := New(r); !errors.Is(err, ErrInvalidRoot) {
				t.Errorf("New(%q) error = %v, want ErrInvalidRoot", r, err)
			}
		}
	})
	t.Run("rejects a kind directory that is a file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "data")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "recordings"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if s, err := New(root); err == nil {
			_ = s.Close()
			t.Fatal("New succeeded with a file in place of a kind directory")
		}
	})
}

func TestWriteOpenSizeRoundTrip(t *testing.T) {
	big := strings.Repeat("0123456789abcdef", 1<<16) // 1 MiB
	tests := []struct {
		name    string
		kind    shared.ArtifactKind
		path    string
		content string
	}{
		{"recording chunk", shared.ArtifactRecordings, "r1/chunk-0001.wav", "audio"},
		{"nested directories", shared.ArtifactTranscripts, "r1/t1/part/0001.json", "{}"},
		{"empty file", shared.ArtifactExports, "empty.txt", ""},
		{"large file", shared.ArtifactModels, "ggml-small.bin", big},
		{"unicode name", shared.ArtifactExports, "дз-по-матану.md", "текст"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newStore(t)
			r := ref(t, tc.kind, tc.path)
			put(t, s, r, tc.content)
			if got := read(t, s, r); got != tc.content {
				t.Fatalf("content mismatch (len %d, want %d)", len(got), len(tc.content))
			}
			if n, err := s.Size(context.Background(), r); err != nil || n != int64(len(tc.content)) {
				t.Fatalf("Size = %d, %v; want %d", n, err, len(tc.content))
			}
			put(t, s, r, "second") // overwrite
			if got := read(t, s, r); got != "second" {
				t.Fatalf("after overwrite = %q", got)
			}
			if tmp := tempFiles(t, root); len(tmp) != 0 {
				t.Fatalf("temp files left: %v", tmp)
			}
		})
	}
}

func TestMissingArtifacts(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	missing := ref(t, shared.ArtifactRecordings, "nope/file")
	if _, err := s.Open(ctx, missing); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("Open error = %v, want ErrNotFound", err)
	}
	if _, err := s.Size(ctx, missing); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("Size error = %v, want ErrNotFound", err)
	}
	if _, err := s.List(ctx, ref(t, shared.ArtifactRecordings, "nope")); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("List error = %v, want ErrNotFound", err)
	}
	// A file where a directory is expected reads as not found.
	put(t, s, ref(t, shared.ArtifactRecordings, "f"), "x")
	if _, err := s.Open(ctx, ref(t, shared.ArtifactRecordings, "f/child")); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("Open under a file error = %v, want ErrNotFound", err)
	}
}

func TestWrongEntryKind(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	put(t, s, ref(t, shared.ArtifactRecordings, "dir/file"), "x")
	dir := ref(t, shared.ArtifactRecordings, "dir")
	file := ref(t, shared.ArtifactRecordings, "dir/file")

	if _, err := s.Open(ctx, dir); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("Open(dir) error = %v, want ErrConflict", err)
	}
	if _, err := s.Size(ctx, dir); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("Size(dir) error = %v, want ErrConflict", err)
	}
	if err := s.Write(ctx, dir, strings.NewReader("y")); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("Write over dir error = %v, want ErrConflict", err)
	}
	if err := s.Write(ctx, ref(t, shared.ArtifactRecordings, "dir/file/sub"), strings.NewReader("y")); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("Write under file error = %v, want ErrConflict", err)
	}
	if _, err := s.List(ctx, file); !errors.Is(err, shared.ErrConflict) {
		t.Errorf("List(file) error = %v, want ErrConflict", err)
	}
	if got := read(t, s, file); got != "x" {
		t.Errorf("file changed: %q", got)
	}
}

type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestWriteFailureKeepsTarget(t *testing.T) {
	boom := errors.New("source broke")
	s, root := newStore(t)
	target := ref(t, shared.ArtifactTranscripts, "t1/out.json")
	put(t, s, target, "old")

	err := s.Write(context.Background(), target, &failingReader{data: []byte("partial-new-content"), err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("Write error = %v, want the source error", err)
	}
	if got := read(t, s, target); got != "old" {
		t.Fatalf("target = %q after a failed write, want %q", got, "old")
	}
	if tmp := tempFiles(t, root); len(tmp) != 0 {
		t.Fatalf("temp files left: %v", tmp)
	}

	// The same for a target that did not exist: nothing appears.
	fresh := ref(t, shared.ArtifactTranscripts, "t1/fresh.json")
	if err := s.Write(context.Background(), fresh, &failingReader{data: []byte("x"), err: boom}); !errors.Is(err, boom) {
		t.Fatalf("Write error = %v, want the source error", err)
	}
	if exists(t, s, fresh) {
		t.Fatal("a failed write created the target")
	}
}

func TestWriteCancelledContext(t *testing.T) {
	s, root := newStore(t)
	target := ref(t, shared.ArtifactExports, "a.txt")
	put(t, s, target, "old")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Write(ctx, target, strings.NewReader("new")); !errors.Is(err, context.Canceled) {
		t.Errorf("Write with a cancelled context = %v", err)
	}
	// Cancelled in the middle of the copy.
	ctx2, cancel2 := context.WithCancel(context.Background())
	r := io.MultiReader(strings.NewReader("part"), readerFunc(func([]byte) (int, error) {
		cancel2()
		return 0, nil
	}), strings.NewReader("more"))
	if err := s.Write(ctx2, target, r); !errors.Is(err, context.Canceled) {
		t.Errorf("Write cancelled mid-copy = %v", err)
	}
	if got := read(t, s, target); got != "old" {
		t.Errorf("target = %q, want old", got)
	}
	if tmp := tempFiles(t, root); len(tmp) != 0 {
		t.Errorf("temp files left: %v", tmp)
	}
	if _, err := s.Open(ctx, target); !errors.Is(err, context.Canceled) {
		t.Errorf("Open with a cancelled context = %v", err)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// crashDuringWrite runs a write that "crashes" (panics) after the temp file is complete and before the rename.
func crashDuringWrite(t *testing.T, s *Store, r shared.ArtifactRef, content string) {
	t.Helper()
	s.beforeRename = func() { panic("simulated crash") }
	defer func() { s.beforeRename = nil }()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the write did not reach the crash point")
			}
		}()
		_ = s.Write(context.Background(), r, strings.NewReader(content))
	}()
}

func TestCrashBetweenTempWriteAndRename(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	existing := ref(t, shared.ArtifactTranscripts, "t1/out.json")
	fresh := ref(t, shared.ArtifactTranscripts, "t1/new.json")
	put(t, s, existing, "old")

	crashDuringWrite(t, s, existing, "NEW-CONTENT")
	crashDuringWrite(t, s, fresh, "NEW-FILE")

	if got := read(t, s, existing); got != "old" {
		t.Fatalf("existing target = %q after a crash, want old", got)
	}
	if exists(t, s, fresh) {
		t.Fatal("a crashed first write made the target visible")
	}
	if n := len(tempFiles(t, root)); n != 2 {
		t.Fatalf("expected 2 leftover temp files, found %d", n)
	}

	list, err := s.List(ctx, ref(t, shared.ArtifactTranscripts, "t1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != existing {
		t.Fatalf("List = %v, want only %s", list, existing)
	}

	// A new store on the same root (restart) cleans the leftovers and keeps the data.
	s2, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	n, err := s2.CleanupTemp()
	if err != nil || n != 2 {
		t.Fatalf("CleanupTemp = %d, %v; want 2, nil", n, err)
	}
	if left := tempFiles(t, root); len(left) != 0 {
		t.Fatalf("temp files after cleanup: %v", left)
	}
	if got := read(t, s2, existing); got != "old" {
		t.Fatalf("existing target = %q after cleanup", got)
	}
	if n, err := s2.CleanupTemp(); err != nil || n != 0 {
		t.Fatalf("second CleanupTemp = %d, %v", n, err)
	}
}

func TestTempNamesAreReserved(t *testing.T) {
	s, _ := newStore(t)
	r := ref(t, shared.ArtifactRecordings, "r1/"+tmpPrefix+"deadbeef")
	ctx := context.Background()
	if err := s.Write(ctx, r, strings.NewReader("x")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Write error = %v, want ErrInvalidArtifactRef", err)
	}
	if _, err := s.Open(ctx, r); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Open error = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Delete(ctx, r); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Delete error = %v, want ErrInvalidArtifactRef", err)
	}
}

func TestZeroReference(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	var z shared.ArtifactRef
	if _, err := s.Open(ctx, z); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Open(zero) = %v", err)
	}
	if err := s.Write(ctx, z, strings.NewReader("x")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Write(zero) = %v", err)
	}
	if err := s.Delete(ctx, z); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Delete(zero) = %v", err)
	}
	if _, err := s.List(ctx, z); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("List(zero) = %v", err)
	}
}

func TestPathTraversalAttempts(t *testing.T) {
	s, root := newStore(t)
	parent := filepath.Dir(root)
	ctx := context.Background()
	// A file outside the root that no operation may touch.
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		"../x", "../../x", "a/../../x", "a/../x", "..", ".", "./x", "x/.",
		"/abs", "/etc/passwd", "//server/share/x", `\x`, `..\x`, `a\..\..\x`,
		"C:/x", `C:\x`, "C:x", "a/C:/x",
		"f.txt:stream", "f.txt::$DATA", "a:b/c",
		"CON", "con", "NUL.txt", "a/aux", "COM1", "lpt9.log", "Prn ",
		"trailing.", "trailing ", "a//b", "", "a/", "x\x00y", "x*y", "x?y", "x|y", `x"y`, "x<y", "x>y",
	}
	for _, p := range paths {
		t.Run(fmt.Sprintf("%q", p), func(t *testing.T) {
			r, err := shared.NewArtifactRef(shared.ArtifactRecordings, p)
			if err == nil {
				t.Fatalf("NewArtifactRef accepted %q", p)
			}
			if !errors.Is(err, shared.ErrInvalidArtifactRef) {
				t.Fatalf("error = %v, want ErrInvalidArtifactRef", err)
			}
			// The zero value that the failed constructor returns must not be usable either.
			if werr := s.Write(ctx, r, strings.NewReader("pwned")); !errors.Is(werr, shared.ErrInvalidArtifactRef) {
				t.Errorf("Write(zero ref) = %v", werr)
			}
			if derr := s.Delete(ctx, r); !errors.Is(derr, shared.ErrInvalidArtifactRef) {
				t.Errorf("Delete(zero ref) = %v", derr)
			}
		})
	}
	for _, s := range []string{"recordings/../x", "../recordings/x", "/recordings/x", "unknown/x", "recordings", ""} {
		if _, err := shared.ParseArtifactRef(s); !errors.Is(err, shared.ErrInvalidArtifactRef) {
			t.Errorf("ParseArtifactRef(%q) error = %v", s, err)
		}
	}

	if b, err := os.ReadFile(outside); err != nil || string(b) != "secret" {
		t.Errorf("outside file changed: %q, %v", b, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 {
		t.Errorf("entries next to the root = %v, want only data and outside.txt", names)
	}
}

func TestLocateRejectsEscapes(t *testing.T) {
	// locate re-validates what the constructor already checked; every valid reference stays under the root.
	for _, p := range []string{"a", "a/b", "дз/файл.txt", ".hidden", "a.b/c.d"} {
		parts, err := locate(ref(t, shared.ArtifactExports, p))
		if err != nil {
			t.Fatalf("locate(%q): %v", p, err)
		}
		if !filepath.IsLocal(filepath.Join(parts...)) || parts[0] != string(shared.ArtifactExports) {
			t.Errorf("locate(%q) = %v", p, parts)
		}
	}
}

// linkTests runs the link refusal checks with a link at recordings/link that points to outside. mk creates it.
func linkTests(t *testing.T, mk func(t *testing.T, link, target string)) {
	t.Helper()
	s, root := newStore(t)
	ctx := context.Background()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(root, "recordings", "link"), outside)

	via := ref(t, shared.ArtifactRecordings, "link/secret.txt")
	viaNew := ref(t, shared.ArtifactRecordings, "link/new.txt")
	link := ref(t, shared.ArtifactRecordings, "link")

	if _, err := s.Open(ctx, via); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Open through link = %v, want ErrInvalidArtifactRef", err)
	}
	if _, err := s.Size(ctx, via); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Size through link = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Write(ctx, viaNew, strings.NewReader("pwned")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Write through link = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Write(ctx, link, strings.NewReader("pwned")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Write onto link = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Delete(ctx, via); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Delete through link = %v, want ErrInvalidArtifactRef", err)
	}
	if _, err := s.List(ctx, link); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("List of link = %v, want ErrInvalidArtifactRef", err)
	}

	if b, err := os.ReadFile(secret); err != nil || string(b) != "secret" {
		t.Errorf("outside file changed: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Error("a file was created outside the root")
	}
	if tmp := tempFiles(t, outside); len(tmp) != 0 {
		t.Errorf("temp files outside the root: %v", tmp)
	}

	// Deleting the link removes only the link.
	if err := s.Delete(ctx, link); err != nil {
		t.Fatalf("Delete(link) = %v", err)
	}
	if b, err := os.ReadFile(secret); err != nil || string(b) != "secret" {
		t.Errorf("deleting the link touched the target: %q, %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "recordings", "link")); err == nil {
		t.Error("the link is still there")
	}
}

func TestLinkOutOfRootIsRefused(t *testing.T) {
	linkTests(t, makeDirLink)
}

func TestLinkInsideRootIsRefused(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	put(t, s, ref(t, shared.ArtifactRecordings, "real/file"), "x")
	makeDirLink(t, filepath.Join(root, "recordings", "alias"), filepath.Join(root, "recordings", "real"))
	if _, err := s.Open(ctx, ref(t, shared.ArtifactRecordings, "alias/file")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Open through an inner link = %v, want ErrInvalidArtifactRef", err)
	}
	list, err := s.List(ctx, ref(t, shared.ArtifactRecordings, "real"))
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
}

func TestListSkipsLinks(t *testing.T) {
	s, root := newStore(t)
	put(t, s, ref(t, shared.ArtifactRecordings, "d/a"), "x")
	makeDirLink(t, filepath.Join(root, "recordings", "d", "b"), t.TempDir())
	list, err := s.List(context.Background(), ref(t, shared.ArtifactRecordings, "d"))
	if err != nil || len(list) != 1 || list[0].Path() != "d/a" {
		t.Fatalf("List = %v, %v; want only d/a", list, err)
	}
}

func TestList(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, n := range []string{"chunk-0003", "chunk-0001", "chunk-0002"} {
		put(t, s, ref(t, shared.ArtifactRecordings, "r1/"+n), n)
	}
	put(t, s, ref(t, shared.ArtifactRecordings, "r1/sub/deep"), "x")
	put(t, s, ref(t, shared.ArtifactRecordings, "r2/other"), "x")

	got, err := s.List(ctx, ref(t, shared.ArtifactRecordings, "r1"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.String())
	}
	want := "recordings/r1/chunk-0001 recordings/r1/chunk-0002 recordings/r1/chunk-0003 recordings/r1/sub"
	if strings.Join(names, " ") != want {
		t.Errorf("List = %v", names)
	}

	empty := ref(t, shared.ArtifactRecordings, "r3")
	put(t, s, ref(t, shared.ArtifactRecordings, "r3/x"), "x")
	if err := s.Delete(ctx, ref(t, shared.ArtifactRecordings, "r3/x")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.List(ctx, empty); err != nil || len(got) != 0 {
		t.Errorf("List(empty dir) = %v, %v", got, err)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("file", func(t *testing.T) {
		s, _ := newStore(t)
		r := ref(t, shared.ArtifactModels, "m.bin")
		put(t, s, r, "x")
		if err := s.Delete(ctx, r); err != nil {
			t.Fatal(err)
		}
		if exists(t, s, r) {
			t.Fatal("file still there")
		}
	})
	t.Run("idempotent for missing targets", func(t *testing.T) {
		s, _ := newStore(t)
		put(t, s, ref(t, shared.ArtifactRecordings, "file"), "x")
		for _, r := range []shared.ArtifactRef{
			ref(t, shared.ArtifactRecordings, "nope"),
			ref(t, shared.ArtifactRecordings, "nope/deeper/still"),
			ref(t, shared.ArtifactRecordings, "file/under-a-file"),
		} {
			if err := s.Delete(ctx, r); err != nil {
				t.Errorf("Delete(%s) = %v", r, err)
			}
			if err := s.Delete(ctx, r); err != nil {
				t.Errorf("second Delete(%s) = %v", r, err)
			}
		}
	})
	t.Run("directory tree leaves siblings", func(t *testing.T) {
		s, _ := newStore(t)
		for _, p := range []string{"r1/chunks/c1", "r1/chunks/c2", "r1/final.wav", "r1/deep/er/x"} {
			put(t, s, ref(t, shared.ArtifactRecordings, p), "x")
		}
		put(t, s, ref(t, shared.ArtifactRecordings, "r10/final.wav"), "keep") // prefix lookalike
		put(t, s, ref(t, shared.ArtifactRecordings, "r2/final.wav"), "keep")
		if err := s.Delete(ctx, ref(t, shared.ArtifactRecordings, "r1")); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"r1", "r1/chunks/c1", "r1/final.wav", "r1/deep/er/x"} {
			if exists(t, s, ref(t, shared.ArtifactRecordings, p)) {
				t.Errorf("%s survived", p)
			}
		}
		if _, err := s.List(ctx, ref(t, shared.ArtifactRecordings, "r1")); !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("List(deleted dir) = %v, want ErrNotFound", err)
		}
		for _, p := range []string{"r10/final.wav", "r2/final.wav"} {
			if got := read(t, s, ref(t, shared.ArtifactRecordings, p)); got != "keep" {
				t.Errorf("%s = %q", p, got)
			}
		}
	})
	t.Run("recording with transcripts and derived artifacts", func(t *testing.T) {
		// What the recording deletion flow does: delete the chunk directory and the final track, then every
		// transcript file listed by the repository. Nothing of the recording may remain.
		s, root := newStore(t)
		chunks := ref(t, shared.ArtifactRecordings, "r1/chunks")
		final := ref(t, shared.ArtifactRecordings, "r1/final.wav")
		transcriptFiles := []shared.ArtifactRef{
			ref(t, shared.ArtifactTranscripts, "t1/transcript.json"),
			ref(t, shared.ArtifactTranscripts, "t1/parts/0001.json"),
			ref(t, shared.ArtifactExports, "t1/summary.md"),
		}
		put(t, s, ref(t, shared.ArtifactRecordings, "r1/chunks/c1"), "a")
		put(t, s, final, "a")
		for _, r := range transcriptFiles {
			put(t, s, r, "t")
		}
		put(t, s, ref(t, shared.ArtifactTranscripts, "t2/transcript.json"), "other")

		for _, r := range append([]shared.ArtifactRef{chunks, final}, transcriptFiles...) {
			if err := s.Delete(ctx, r); err != nil {
				t.Fatalf("Delete(%s): %v", r, err)
			}
		}
		if exists(t, s, final) || exists(t, s, transcriptFiles[0]) || exists(t, s, transcriptFiles[2]) {
			t.Error("artifacts of the recording survived")
		}
		if got := read(t, s, ref(t, shared.ArtifactTranscripts, "t2/transcript.json")); got != "other" {
			t.Errorf("unrelated transcript = %q", got)
		}
		// Deleting the whole transcript directory removes nested derived files at once.
		if err := s.Delete(ctx, ref(t, shared.ArtifactTranscripts, "t2")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "transcripts", "t2")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("t2 directory: %v", err)
		}
	})
	t.Run("temp leftovers inside a deleted directory go too", func(t *testing.T) {
		s, root := newStore(t)
		target := ref(t, shared.ArtifactRecordings, "r1/x")
		put(t, s, target, "x")
		crashDuringWrite(t, s, ref(t, shared.ArtifactRecordings, "r1/y"), "y")
		if err := s.Delete(ctx, ref(t, shared.ArtifactRecordings, "r1")); err != nil {
			t.Fatal(err)
		}
		if tmp := tempFiles(t, root); len(tmp) != 0 {
			t.Errorf("temp files left: %v", tmp)
		}
	})
}

func TestErrorsDoNotLeakPaths(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	r := ref(t, shared.ArtifactRecordings, "a/b")
	_, err1 := s.Open(ctx, r)
	_, err2 := s.List(ctx, ref(t, shared.ArtifactRecordings, "a"))
	err3 := mapErr("op", r, &fs.PathError{Op: "open", Path: root + "/recordings/a/b", Err: errors.New("access denied")})
	for _, err := range []error{err1, err2, err3} {
		if err == nil || strings.Contains(err.Error(), root) {
			t.Errorf("error %v mentions the root path or is nil", err)
		}
		var pe *fs.PathError
		if errors.As(err, &pe) {
			t.Errorf("error %v exposes *fs.PathError", err)
		}
	}
	if !errors.Is(err3, ErrStorage) {
		t.Errorf("mapErr = %v, want ErrStorage", err3)
	}
}

func TestConcurrentWrites(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	same := ref(t, shared.ArtifactExports, "same.txt")
	const writers = 16
	const rounds = 5

	payload := func(i int) string { return strings.Repeat(fmt.Sprintf("<%02d>", i), 20000) }
	valid := map[string]bool{}
	for i := 0; i < writers; i++ {
		valid[payload(i)] = true
	}
	put(t, s, same, payload(0))

	var wg sync.WaitGroup
	errc := make(chan error, writers*rounds*3)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			own := ref(t, shared.ArtifactExports, fmt.Sprintf("dir/own-%02d.txt", i))
			for r := 0; r < rounds; r++ {
				if err := s.Write(ctx, same, strings.NewReader(payload(i))); err != nil {
					errc <- fmt.Errorf("write same: %w", err)
				}
				if err := s.Write(ctx, own, strings.NewReader(payload(i))); err != nil {
					errc <- fmt.Errorf("write own: %w", err)
				}
				// A reader sees a whole payload, never a torn one.
				rc, err := s.Open(ctx, same)
				if err != nil {
					errc <- fmt.Errorf("open same: %w", err)
					continue
				}
				b, err := io.ReadAll(rc)
				_ = rc.Close()
				if err != nil || !valid[string(b)] {
					errc <- fmt.Errorf("torn or unexpected read: len=%d err=%v", len(b), err)
				}
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}

	if got := read(t, s, same); !valid[got] {
		t.Errorf("final content is torn (len %d)", len(got))
	}
	list, err := s.List(ctx, ref(t, shared.ArtifactExports, "dir"))
	if err != nil || len(list) != writers {
		t.Errorf("List = %d entries, %v; want %d", len(list), err, writers)
	}
	for i := 0; i < writers; i++ {
		if got := read(t, s, ref(t, shared.ArtifactExports, fmt.Sprintf("dir/own-%02d.txt", i))); got != payload(i) {
			t.Errorf("own-%02d mismatch", i)
		}
	}
	if tmp := tempFiles(t, root); len(tmp) != 0 {
		t.Errorf("temp files left: %v", tmp)
	}
}

func TestWriteReadsContentOnce(t *testing.T) {
	s, _ := newStore(t)
	var buf bytes.Buffer
	buf.WriteString("hello")
	r := ref(t, shared.ArtifactExports, "h.txt")
	if err := s.Write(context.Background(), r, &buf); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s, r); got != "hello" {
		t.Errorf("got %q", got)
	}
}
