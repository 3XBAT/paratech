package textfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secret-student")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// onlyFiles returns the names in dir, so a test sees a leftover temporary file.
func onlyFiles(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestWriteFile(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "Физика 2026-10-19.txt")
	for _, content := range []string{"первая версия\n", "вторая\n"} { // a second save replaces the file
		if err := New().WriteFile(context.Background(), path, []byte(content)); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Fatalf("file = %q, %v", got, err)
		}
	}
	if names := onlyFiles(t, dir); len(names) != 1 {
		t.Fatalf("files = %v, want only the target", names)
	}
}

func TestWriteFileFailures(t *testing.T) {
	dir := privateDir(t)
	existing := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "folder.txt")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		path string
		data []byte
		want error
	}{
		{"relative path", context.Background(), "out.txt", []byte("x"), ErrWrite},
		{"empty path", context.Background(), "", []byte("x"), ErrWrite},
		{"nul in path", context.Background(), filepath.Join(dir, "a\x00b.txt"), []byte("x"), ErrWrite},
		{"directory target", context.Background(), sub, []byte("x"), ErrNotRegular},
		{"missing folder", context.Background(), filepath.Join(dir, "missing", "out.txt"), []byte("x"), ErrWrite},
		{"too large", context.Background(), existing, make([]byte, MaxBytes+1), ErrTooLarge},
		{"cancelled", cancelled, existing, []byte("x"), context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := New().WriteFile(tc.ctx, tc.path, tc.data)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret-student") {
				t.Fatalf("error names the path: %v", err)
			}
		})
	}

	if got, _ := os.ReadFile(existing); string(got) != "old" {
		t.Fatalf("existing file changed to %q", got)
	}
	if names := onlyFiles(t, dir); len(names) != 2 {
		t.Fatalf("files = %v, want no leftovers", names)
	}
}
