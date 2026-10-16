package zipfile

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func entries(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func fillWith(files ...string) func(add func(string, []byte) error) error {
	return func(add func(string, []byte) error) error {
		for i := 0; i+1 < len(files); i += 2 {
			if err := add(files[i], []byte(files[i+1])); err != nil {
				return err
			}
		}
		return nil
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secret-student")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteZip(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "bundle.zip")
	if err := New().WriteZip(context.Background(), path, fillWith("about.json", "{}", "logs/desktop.log", "line\n")); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, path); len(got) != 2 || got["about.json"] != "{}" || got["logs/desktop.log"] != "line\n" {
		t.Fatalf("entries %v", got)
	}

	// Overwriting an existing archive (the dialog asked) replaces it whole.
	if err := New().WriteZip(context.Background(), path, fillWith("only.txt", "x")); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, path); len(got) != 1 || got["only.txt"] != "x" {
		t.Fatalf("entries after the overwrite %v", got)
	}
	assertCount(t, dir, 1)
}

// A failure removes the temporary file and leaves the target as it was; no error names the path.
func TestWriteZipFailures(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	tests := []struct {
		name string
		path string
		fill func(add func(string, []byte) error) error
		want error
	}{
		{"fill fails", path, func(func(string, []byte) error) error { return boom }, boom},
		{"bad entry name", path, fillWith("../x", "y"), ErrEntryName},
		{"target is a directory", dir, fillWith("a", "b"), ErrNotRegular},
		{"relative path", "bundle.zip", fillWith("a", "b"), ErrWrite},
		{"missing directory", filepath.Join(dir, "none", "bundle.zip"), fillWith("a", "b"), ErrWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New().WriteZip(context.Background(), tt.path, tt.fill)
			if !errors.Is(err, tt.want) || strings.Contains(err.Error(), "secret-student") {
				t.Fatalf("WriteZip = %v, want %v without the path", err, tt.want)
			}
		})
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "old" {
		t.Fatalf("the target changed: %q, %v", b, err)
	}
	assertCount(t, dir, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New().WriteZip(ctx, path, fillWith("a", "b")); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled write = %v", err)
	}
}

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"about.json", "logs/desktop.log", "logs/desktop.1.log", "a..b"} {
		if err := CheckName(ok); err != nil {
			t.Errorf("CheckName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "/etc/passwd", "../x", "logs/../../x", `logs\x`, "C:x", "C:/x", "a//b", "./a", "a/", "a\x00b", "a\nb",
	} {
		if err := CheckName(bad); !errors.Is(err, ErrEntryName) {
			t.Errorf("CheckName(%q) = %v, want ErrEntryName", bad, err)
		}
	}
}

// assertCount checks that no temporary file was left in dir.
func assertCount(t *testing.T, dir string, n int) {
	t.Helper()
	got, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("the directory holds %v, want %d entries", got, n)
	}
}
