//go:build windows

package localfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

// TestOpenRefusesNonLocalWindowsPaths: paths that would reach a remote host, a device or a stream are refused before
// any file system call. The hosts are .invalid, so a regression fails fast instead of contacting the network.
func TestOpenRefusesNonLocalWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(file, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
	}{
		{"unc", `\\host.invalid\share\model.bin`},
		{"unc with slashes", `//host.invalid/share/model.bin`},
		{"unc ip", `\\192.0.2.1\c$\model.bin`},
		{"unc long path", `\\?\UNC\host.invalid\share\model.bin`},
		{"device namespace", `\\.\C:\model.bin`},
		{"device", `\\.\PhysicalDrive0`},
		{"win32 namespace", `\\?\` + file},
		{"nt namespace", `\??\` + file},
		{"rooted without drive", `\Users\model.bin`},
		{"drive relative", `C:model.bin`},
		{"drive only", `C:`},
		{"drive root", `C:\`},
		{"reserved name", filepath.Join(dir, "NUL")},
		{"reserved name lower case", filepath.Join(dir, "com1")},
		{"reserved console", filepath.Join(dir, "CONIN$")},
		{"reserved directory element", filepath.Join(dir, "AUX", "model.bin")},
		{"alternate data stream", file + ":hidden"},
		{"default data stream", file + "::$DATA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc, _, err := New().Open(context.Background(), tc.path)
			if rc != nil {
				_ = rc.Close()
			}
			if !errors.Is(err, values.ErrInvalidImportFile) {
				t.Fatalf("Open(%q) = %v, want values.ErrInvalidImportFile", tc.path, err)
			}
			if strings.Contains(err.Error(), "host.invalid") || strings.Contains(err.Error(), dir) {
				t.Fatalf("the error names the path: %v", err)
			}
		})
	}
}

// TestOpenAcceptsDrivePaths: a drive letter path in either case and with forward slashes is a local file.
func TestOpenAcceptsDrivePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(file, []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		file,
		filepath.ToSlash(file),
		strings.ToLower(file[:1]) + file[1:],
		strings.ToUpper(file[:1]) + file[1:],
	} {
		rc, _, err := New().Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open(%q) = %v", path, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIsDriveLetter(t *testing.T) {
	cases := map[string]bool{
		"C:": true, "c:": true, "Z:": true, "a:": true,
		"": false, "C": false, "1:": false, "@:": false, "[:": false, "C:\\": false, `\\h\s`: false,
	}
	for vol, want := range cases {
		if got := isDriveLetter(vol); got != want {
			t.Errorf("isDriveLetter(%q) = %v, want %v", vol, got, want)
		}
	}
}
