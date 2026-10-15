package userfile

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const limit = 16

// writeFile stores data as name in a directory named secret-folder and returns its path.
func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secret-folder")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadFileReadsUpToOneBytePastTheLimit(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "empty", size: 0, want: 0},
		{name: "small", size: 3, want: 3},
		{name: "exactly at the limit", size: limit, want: limit},
		{name: "limit plus one", size: limit + 1, want: limit + 1},
		{name: "much larger", size: 10 * limit, want: limit + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := bytes.Repeat([]byte("a"), tt.size)
			got, err := New().ReadFile(context.Background(), writeFile(t, "a.csv", data), limit)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data[:tt.want]) {
				t.Fatalf("read %d bytes, want %d", len(got), tt.want)
			}
		})
	}
}

func TestReadFileRefusals(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "secret-folder", "gone.csv")
	dir := filepath.Join(t.TempDir(), "secret-folder.csv")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := writeFile(t, "a.csv", []byte("x"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name  string
		ctx   context.Context
		path  string
		limit int64
		want  []error
	}{
		{name: "missing file", path: missing, limit: limit, want: []error{ErrUnreadable, fs.ErrNotExist}},
		{name: "a directory", path: dir, limit: limit, want: []error{ErrNotRegular}},
		{name: "empty path", path: "", limit: limit, want: []error{ErrUnreadable}},
		{name: "NUL in the path", path: file + "\x00secret-folder", limit: limit, want: []error{ErrUnreadable}},
		{name: "negative limit", path: file, limit: -1, want: []error{ErrUnreadable}},
		{name: "cancelled", ctx: cancelled, path: file, limit: limit, want: []error{context.Canceled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			data, err := New().ReadFile(ctx, tt.path, tt.limit)
			if err == nil || data != nil {
				t.Fatalf("ReadFile = %q, %v; want an error", data, err)
			}
			for _, want := range tt.want {
				if !errors.Is(err, want) {
					t.Errorf("err = %v, want errors.Is %v", err, want)
				}
			}
			if strings.Contains(err.Error(), "secret-folder") {
				t.Fatalf("the path leaked: %q", err)
			}
		})
	}
}
