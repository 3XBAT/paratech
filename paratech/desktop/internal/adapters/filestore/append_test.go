package filestore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestAppend(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	part := ref(t, shared.ArtifactModels, "downloads/m.partial")

	n, err := s.Append(ctx, part, strings.NewReader("abc"))
	if err != nil || n != 3 {
		t.Fatalf("Append to a new file = %d, %v", n, err)
	}
	n, err = s.Append(ctx, part, strings.NewReader("def"))
	if err != nil || n != 3 {
		t.Fatalf("second Append = %d, %v", n, err)
	}
	if got := read(t, s, part); got != "abcdef" {
		t.Fatalf("content = %q, want abcdef", got)
	}

	// A broken source keeps what arrived and reports the source error.
	boom := errors.New("connection reset")
	n, err = s.Append(ctx, part, &failingReader{data: []byte("gh"), err: boom})
	if !errors.Is(err, boom) || n != 2 {
		t.Fatalf("Append with a broken source = %d, %v; want 2 and the source error", n, err)
	}
	if got := read(t, s, part); got != "abcdefgh" {
		t.Fatalf("content after a broken source = %q", got)
	}

	// Cancellation in the middle keeps the bytes before it.
	cctx, cancel := context.WithCancel(ctx)
	r := io.MultiReader(strings.NewReader("ij"), readerFunc(func([]byte) (int, error) {
		cancel()
		return 0, nil
	}), strings.NewReader("never"))
	if _, err := s.Append(cctx, part, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append cancelled mid-copy = %v", err)
	}
	if got := read(t, s, part); got != "abcdefghij" {
		t.Fatalf("content after cancel = %q", got)
	}
	if _, err := s.Append(cctx, part, strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append with a cancelled context = %v", err)
	}

	if _, err := s.Append(ctx, ref(t, shared.ArtifactModels, "downloads"), strings.NewReader("x")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Append onto a directory = %v, want ErrConflict", err)
	}
	if _, err := s.Append(ctx, ref(t, shared.ArtifactModels, "downloads/m.partial/x"), strings.NewReader("x")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("Append below a file = %v, want ErrConflict", err)
	}
	if _, err := s.Append(ctx, shared.ArtifactRef{}, strings.NewReader("x")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("Append to the zero reference = %v", err)
	}
}

func TestRename(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	part := ref(t, shared.ArtifactModels, "downloads/m.partial")
	final := ref(t, shared.ArtifactModels, "m-1.bin")
	put(t, s, part, "model")

	if err := s.Rename(ctx, part, final); err != nil {
		t.Fatalf("Rename = %v", err)
	}
	if exists(t, s, part) || read(t, s, final) != "model" {
		t.Fatal("Rename did not move the file")
	}

	tests := []struct {
		name     string
		from, to shared.ArtifactRef
		want     error
	}{
		{"missing source", part, ref(t, shared.ArtifactModels, "x.bin"), shared.ErrNotFound},
		{"existing target", final, final, shared.ErrConflict},
		{"directory source", ref(t, shared.ArtifactModels, "downloads"), ref(t, shared.ArtifactModels, "d"), shared.ErrConflict},
		{"target below a file", final, ref(t, shared.ArtifactModels, "m-1.bin/x"), shared.ErrConflict},
		{"zero target", final, shared.ArtifactRef{}, shared.ErrInvalidArtifactRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Rename(ctx, tc.from, tc.to); !errors.Is(err, tc.want) {
				t.Fatalf("Rename = %v, want %v", err, tc.want)
			}
		})
	}
	if read(t, s, final) != "model" {
		t.Fatal("a refused rename changed the file")
	}

	// A new directory of the target is created.
	nested := ref(t, shared.ArtifactModels, "base/m-2.bin")
	if err := s.Rename(ctx, final, nested); err != nil || read(t, s, nested) != "model" {
		t.Fatalf("Rename into a new directory = %v", err)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Rename(cctx, nested, final); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rename with a cancelled context = %v", err)
	}
}

func TestAppendAndRenameRefuseLinks(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	outside := t.TempDir()
	makeDirLink(t, filepath.Join(root, "models", "link"), outside)
	put(t, s, ref(t, shared.ArtifactModels, "m.bin"), "model")

	if _, err := s.Append(ctx, ref(t, shared.ArtifactModels, "link/p.partial"), strings.NewReader("x")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Append through a link = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Rename(ctx, ref(t, shared.ArtifactModels, "m.bin"), ref(t, shared.ArtifactModels, "link/m.bin")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Rename into a link = %v, want ErrInvalidArtifactRef", err)
	}
	if err := s.Rename(ctx, ref(t, shared.ArtifactModels, "link"), ref(t, shared.ArtifactModels, "moved")); !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Errorf("Rename of a link = %v, want ErrInvalidArtifactRef", err)
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("files appeared outside the root: %v, %v", entries, err)
	}
}

func TestFreeBytes(t *testing.T) {
	s, _ := newStore(t)
	n, err := s.FreeBytes(context.Background())
	if err != nil || n <= 0 {
		t.Fatalf("FreeBytes = %d, %v; want a positive number", n, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.FreeBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("FreeBytes with a cancelled context = %v", err)
	}
}

func TestCreate(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	partial := ref(t, shared.ArtifactRecordings, "r1/chunk-000001.wav.partial")

	f, err := s.Create(ctx, partial)
	if err != nil {
		t.Fatalf("Create in a new directory = %v", err)
	}
	if _, err := f.Write([]byte("head")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("HEAD"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s, partial); got != "HEAD" {
		t.Fatalf("content = %q, want HEAD", got)
	}

	// The recorder's chunk names are references that List shows.
	listed, err := s.List(ctx, ref(t, shared.ArtifactRecordings, "r1"))
	if err != nil || len(listed) != 1 || listed[0] != partial {
		t.Fatalf("List = %v, %v; want the partial chunk", listed, err)
	}

	put(t, s, ref(t, shared.ArtifactRecordings, "file"), "x")
	tests := []struct {
		name string
		ref  shared.ArtifactRef
		want error
	}{
		{"existing file", partial, shared.ErrConflict},
		{"existing directory", ref(t, shared.ArtifactRecordings, "r1"), shared.ErrConflict},
		{"below a file", ref(t, shared.ArtifactRecordings, "file/x"), shared.ErrConflict},
		{"zero reference", shared.ArtifactRef{}, shared.ErrInvalidArtifactRef},
		{"temporary prefix", ref(t, shared.ArtifactRecordings, "r1/"+tmpPrefix+"x"), shared.ErrInvalidArtifactRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := s.Create(ctx, tc.ref)
			if f != nil {
				_ = f.Close()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}
		})
	}
	if got := read(t, s, partial); got != "HEAD" {
		t.Fatalf("a refused Create changed the file: %q", got)
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Create(cctx, ref(t, shared.ArtifactRecordings, "r1/y")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Create with a cancelled context = %v", err)
	}
}

func TestCreateRefusesLinks(t *testing.T) {
	s, root := newStore(t)
	outside := t.TempDir()
	makeDirLink(t, filepath.Join(root, "recordings", "link"), outside)

	f, err := s.Create(context.Background(), ref(t, shared.ArtifactRecordings, "link/chunk-000001.wav.partial"))
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, shared.ErrInvalidArtifactRef) {
		t.Fatalf("Create through a link = %v, want ErrInvalidArtifactRef", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("files appeared outside the root: %v, %v", entries, err)
	}
}
