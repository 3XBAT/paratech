package filestore

import (
	"context"
	"errors"
	"io"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestOpenSeekable(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	r := ref(t, shared.ArtifactRecordings, "r1.wav")
	put(t, s, r, "RIFF0123456789")

	f, size, err := s.OpenSeekable(ctx, r)
	if err != nil {
		t.Fatalf("OpenSeekable: %v", err)
	}
	defer func() { _ = f.Close() }()
	if size != 14 {
		t.Fatalf("size = %d, want 14", size)
	}
	if _, err := f.Seek(10, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(f)
	if err != nil || string(rest) != "6789" {
		t.Fatalf("read after seek = %q, %v", rest, err)
	}
}

func TestOpenSeekableRefusals(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	put(t, s, ref(t, shared.ArtifactRecordings, "dir/inner.wav"), "x")

	if _, _, err := s.OpenSeekable(ctx, ref(t, shared.ArtifactRecordings, "missing.wav")); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing: %v, want ErrNotFound", err)
	}
	if _, _, err := s.OpenSeekable(ctx, ref(t, shared.ArtifactRecordings, "dir")); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("directory: %v, want ErrConflict", err)
	}
	ctxDone, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := s.OpenSeekable(ctxDone, ref(t, shared.ArtifactRecordings, "dir/inner.wav")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
