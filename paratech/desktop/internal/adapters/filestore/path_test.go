package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// paths mirrors the Paths port of the whispercli adapter (adapters do not import each other, ARCH-30).
type paths interface {
	Path(ctx context.Context, ref shared.ArtifactRef) (string, error)
}

var _ paths = (*Store)(nil)

func TestPath(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()
	file := ref(t, shared.ArtifactModels, "dir/ggml-base.bin")
	put(t, s, file, "model")

	tests := []struct {
		name    string
		ref     shared.ArtifactRef
		want    string
		wantErr error
	}{
		{name: "file", ref: file, want: filepath.Join(root, "models", "dir", "ggml-base.bin")},
		{name: "missing", ref: ref(t, shared.ArtifactModels, "dir/none.bin"), wantErr: shared.ErrNotFound},
		{name: "missing parent", ref: ref(t, shared.ArtifactModels, "none/x.bin"), wantErr: shared.ErrNotFound},
		{name: "parent is a file", ref: ref(t, shared.ArtifactModels, "dir/ggml-base.bin/x"), wantErr: shared.ErrNotFound},
		{name: "directory", ref: ref(t, shared.ArtifactModels, "dir"), wantErr: shared.ErrConflict},
		{name: "zero reference", wantErr: shared.ErrInvalidArtifactRef},
		{name: "temporary name", ref: ref(t, shared.ArtifactModels, tmpPrefix+"x"), wantErr: shared.ErrInvalidArtifactRef},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Path(ctx, tc.ref)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Path = %q, %v; want %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("Path = %q, %v; want %q", got, err, tc.want)
			}
			if !filepath.IsAbs(got) || filepath.Clean(got) != got {
				t.Errorf("path %q is not absolute and clean", got)
			}
			if b, err := os.ReadFile(got); err != nil || string(b) != "model" {
				t.Errorf("content at the path = %q, %v", b, err)
			}
		})
	}
}

func TestPathCancelled(t *testing.T) {
	s, _ := newStore(t)
	file := ref(t, shared.ArtifactModels, "m.bin")
	put(t, s, file, "model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Path(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatalf("Path = %v, want context.Canceled", err)
	}
}

// TestPathRefusesLinks checks every component, not only the last one: a link in the middle would let the sidecar
// read a file outside the data root.
func TestPathRefusesLinks(t *testing.T) {
	s, root := newStore(t)
	ctx := context.Background()

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.bin"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	put(t, s, ref(t, shared.ArtifactRecordings, "real/a.wav"), "audio")

	makeDirLink(t, filepath.Join(root, "recordings", "out"), outside)
	makeDirLink(t, filepath.Join(root, "recordings", "alias"), filepath.Join(root, "recordings", "real"))

	for _, r := range []shared.ArtifactRef{
		ref(t, shared.ArtifactRecordings, "out/secret.bin"),
		ref(t, shared.ArtifactRecordings, "alias/a.wav"),
		ref(t, shared.ArtifactRecordings, "out"),
	} {
		if got, err := s.Path(ctx, r); !errors.Is(err, shared.ErrInvalidArtifactRef) {
			t.Errorf("Path(%s) = %q, %v; want ErrInvalidArtifactRef", r, got, err)
		}
	}
}
