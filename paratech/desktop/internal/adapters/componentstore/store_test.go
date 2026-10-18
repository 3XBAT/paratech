package componentstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

func TestNewCreatesTheComponentFolder(t *testing.T) {
	data := t.TempDir()
	s, err := New(data, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if fi, err := os.Lstat(filepath.Join(data, componentsDir)); err != nil || !fi.IsDir() {
		t.Fatalf("components: %v, %v", fi, err)
	}
	// Again on the existing folder.
	s, err = New(data, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

func TestNewRefusesALinkedComponentFolder(t *testing.T) {
	data, elsewhere := t.TempDir(), t.TempDir()
	makeDirLink(t, filepath.Join(data, componentsDir), elsewhere)
	if _, err := New(data, ""); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("New = %v, want ErrInvalidRoot", err)
	}
}

func TestInspect(t *testing.T) {
	spec := specFor(t, buildZip(t, goodEntries()))
	ctx := context.Background()

	t.Run("unknown and missing folders hold nothing", func(t *testing.T) {
		s, _ := newStore(t, "")
		for _, f := range values.ComponentFolders() {
			if found, err := s.Inspect(ctx, f, spec); err != nil || len(found) != 0 {
				t.Fatalf("%v: %v, %v", f, found, err)
			}
		}
		s2, _ := newStore(t, filepath.Join(t.TempDir(), "gone"))
		if found, err := s2.Inspect(ctx, values.FolderBundled, spec); err != nil || len(found) != 0 {
			t.Fatalf("missing bundled folder: %v, %v", found, err)
		}
	})

	t.Run("bundled set verifies", func(t *testing.T) {
		bundled := t.TempDir()
		writeFile(t, filepath.Join(bundled, "whisper-cli.exe"), exeData)
		writeFile(t, filepath.Join(bundled, "whisper.dll"), dllData)
		writeFile(t, filepath.Join(bundled, "learning-assistant.exe"), []byte("the app"))
		s, _ := newStore(t, bundled)
		found, err := s.Inspect(ctx, values.FolderBundled, spec)
		if err != nil || len(found) != 2 || spec.Assess(found) != values.ComponentReady {
			t.Fatalf("Inspect = %v, %v", found, err)
		}
	})

	t.Run("a directory under a file name is not regular", func(t *testing.T) {
		bundled := t.TempDir()
		writeFile(t, filepath.Join(bundled, "whisper-cli.exe"), exeData)
		if err := os.Mkdir(filepath.Join(bundled, "whisper.dll"), 0o700); err != nil {
			t.Fatal(err)
		}
		s, _ := newStore(t, bundled)
		found, err := s.Inspect(ctx, values.FolderBundled, spec)
		if err != nil || spec.Assess(found) != values.ComponentDamaged {
			t.Fatalf("Inspect = %v, %v; want damaged", found, err)
		}
	})

	t.Run("a linked file is not regular", func(t *testing.T) {
		bundled, elsewhere := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(bundled, "whisper-cli.exe"), exeData)
		writeFile(t, filepath.Join(elsewhere, "whisper.dll"), dllData)
		if err := os.Symlink(filepath.Join(elsewhere, "whisper.dll"), filepath.Join(bundled, "whisper.dll")); err != nil {
			t.Skipf("cannot create a file symlink here: %v", err)
		}
		s, _ := newStore(t, bundled)
		found, err := s.Inspect(ctx, values.FolderBundled, spec)
		if err != nil || spec.Assess(found) != values.ComponentDamaged {
			t.Fatalf("Inspect = %v, %v; want damaged", found, err)
		}
	})

	t.Run("a larger file is hashed only up to one byte over its size", func(t *testing.T) {
		bundled := t.TempDir()
		writeFile(t, filepath.Join(bundled, "whisper-cli.exe"), bytes.Repeat([]byte("x"), 10*len(exeData)))
		s, _ := newStore(t, bundled)
		found, err := s.Inspect(ctx, values.FolderBundled, spec)
		if err != nil || len(found) != 1 || found[0].Size != int64(len(exeData))+1 {
			t.Fatalf("Inspect = %v, %v", found, err)
		}
	})

	t.Run("a linked installed folder does not count", func(t *testing.T) {
		s, data := newStore(t, "")
		elsewhere := t.TempDir()
		writeFile(t, filepath.Join(elsewhere, "whisper-cli.exe"), exeData)
		writeFile(t, filepath.Join(elsewhere, "whisper.dll"), dllData)
		makeDirLink(t, filepath.Join(data, componentsDir, installedDir), elsewhere)
		if _, err := s.Inspect(ctx, values.FolderInstalled, spec); !errors.Is(err, ErrLinkedFolder) {
			t.Fatalf("Inspect = %v, want ErrLinkedFolder", err)
		}
	})
}

func TestSaveArchive(t *testing.T) {
	s, data := newStore(t, "")
	ctx := context.Background()
	n, got, err := s.SaveArchive(ctx, bytes.NewReader([]byte("first")))
	if err != nil || n != 5 || got != values.SHA256(sum([]byte("first"))) {
		t.Fatalf("SaveArchive = %d, %s, %v", n, got, err)
	}
	if _, _, err := s.SaveArchive(ctx, bytes.NewReader([]byte("second"))); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, componentsDir, archiveName))
	if err != nil || string(b) != "second" {
		t.Fatalf("stored %q, %v; want the newer archive", b, err)
	}

	broken := io.MultiReader(bytes.NewReader([]byte("part")), errReader{})
	if _, _, err := s.SaveArchive(ctx, broken); err == nil || errors.Is(err, ErrStorage) {
		t.Fatalf("SaveArchive of a broken stream = %v, want the stream's error", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.SaveArchive(cancelled, bytes.NewReader([]byte("x"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("SaveArchive with a cancelled context = %v", err)
	}
	if got := joined(names(t, filepath.Join(data, componentsDir))); got != archiveName {
		t.Fatalf("components holds %s: a failed save must leave no temporary file", got)
	}

	if err := s.DiscardArchive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardArchive(ctx); err != nil {
		t.Fatalf("second DiscardArchive = %v", err)
	}
	if got := names(t, filepath.Join(data, componentsDir)); len(got) != 0 {
		t.Fatalf("components holds %v", got)
	}
}

func TestCleanupRemovesLeftoversOnly(t *testing.T) {
	s, data := newStore(t, "")
	dir := filepath.Join(data, componentsDir)
	writeFile(t, filepath.Join(dir, installedDir, "whisper.dll"), dllData)
	writeFile(t, filepath.Join(dir, archiveName), []byte("zip"))
	writeFile(t, filepath.Join(dir, tmpPrefix+"1"), []byte("tmp"))
	writeFile(t, filepath.Join(dir, stagingPrefix+"1", "whisper.dll"), dllData)
	writeFile(t, filepath.Join(dir, retiredPrefix+"1", "whisper.dll"), dllData)
	writeFile(t, filepath.Join(dir, "other.txt"), []byte("not ours to judge"))

	n, err := s.Cleanup(context.Background())
	if err != nil || n != 4 {
		t.Fatalf("Cleanup = %d, %v; want 4 removed", n, err)
	}
	if got := joined(names(t, dir)); got != "other.txt,"+installedDir {
		t.Fatalf("left %s", got)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
