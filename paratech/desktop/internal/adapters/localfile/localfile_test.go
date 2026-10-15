package localfile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

func TestOpenRegularFile(t *testing.T) {
	content := bytes.Repeat([]byte("model"), 1000)
	path := filepath.Join(t.TempDir(), "ggml-base.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	rc, size, err := New().Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if size != int64(len(content)) || !bytes.Equal(got, content) {
		t.Fatalf("Open = %d bytes reported, %d read; want %d", size, len(got), len(content))
	}

	// The file is only read: it can be removed after Close (Windows keeps open files).
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove after Close: %v", err)
	}
}

func TestOpenRefuses(t *testing.T) {
	dir := t.TempDir()
	device := "/dev/null"
	if runtime.GOOS == "windows" {
		device = `\\.\NUL`
	}

	cases := []struct {
		name string
		path string
		want error
	}{
		{"empty path", "", values.ErrInvalidImportFile},
		{"nul byte", filepath.Join(dir, "model.bin\x00.txt"), values.ErrInvalidImportFile},
		{"relative path", "model.bin", values.ErrInvalidImportFile},
		{"relative path escaping", filepath.Join("..", "..", "model.bin"), values.ErrInvalidImportFile},
		{"relative path with dir", filepath.Join("models", "model.bin"), values.ErrInvalidImportFile},
		{"directory", dir, values.ErrInvalidImportFile},
		{"device", device, values.ErrInvalidImportFile},
		{"missing file", filepath.Join(dir, "missing-secret-name.bin"), shared.ErrNotFound},
		{"missing directory", filepath.Join(dir, "missing-secret-dir", "model.bin"), shared.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, err := New().Open(context.Background(), tc.path)
			if rc != nil {
				_ = rc.Close()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Open(%q) = %v, want %v", tc.path, err, tc.want)
			}
			if strings.Contains(err.Error(), "secret") || (tc.path != "" && strings.Contains(err.Error(), tc.path)) {
				t.Fatalf("the error names the path: %v", err)
			}
			var pe *os.PathError
			if errors.As(err, &pe) {
				t.Fatalf("the error exposes *fs.PathError: %v", err)
			}
		})
	}
}

// TestOpenCleansPath: a path with dot elements and doubled separators is cleaned and opened.
func TestOpenCleansPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)

	rc, size, err := New().Open(context.Background(), dir+sep+"sub"+sep+".."+sep+"."+sep+sep+"model.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if size != int64(len("weights")) {
		t.Fatalf("size = %d", size)
	}
}

func TestOpenHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := New().Open(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open = %v, want context.Canceled", err)
	}
}

func TestMapErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []error
	}{
		{"not found", &os.PathError{Op: "open", Path: `C:\secret\a.bin`, Err: os.ErrNotExist}, []error{shared.ErrNotFound}},
		{"permission", &os.PathError{Op: "open", Path: `C:\secret\a.bin`, Err: os.ErrPermission},
			[]error{ErrStorage, shared.ErrStorage, shared.ErrStoragePermission, os.ErrPermission}},
		{"other", &os.PathError{Op: "read", Path: `C:\secret\a.bin`, Err: io.ErrUnexpectedEOF},
			[]error{ErrStorage, shared.ErrStorage, io.ErrUnexpectedEOF}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapErr("open", tc.err)
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Errorf("mapErr = %v, want errors.Is %v", err, w)
				}
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("the message names the path: %v", err)
			}
		})
	}
	if mapErr("close", nil) != nil {
		t.Fatal("mapErr(nil) != nil")
	}
}
