package componentstore

import (
	"archive/zip"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

func TestUnpackInstallsOnlyTheComponentFiles(t *testing.T) {
	archive := buildZip(t, goodEntries())
	spec := specFor(t, archive)
	s, data := newStore(t, "")
	save(t, s, archive)

	if err := s.Unpack(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(data, componentsDir, installedDir)
	if got := joined(names(t, installed)); got != "whisper-cli.exe,whisper.dll" {
		t.Fatalf("installed %s, want only the component files", got)
	}
	found, err := s.Inspect(context.Background(), values.FolderInstalled, spec)
	if err != nil || spec.Assess(found) != values.ComponentReady {
		t.Fatalf("Inspect = %v, %v; want the verified set", found, err)
	}
	// Only the installed folder and the archive (the use case discards it) are left.
	if got := joined(names(t, filepath.Join(data, componentsDir))); got != installedDir+","+archiveName {
		t.Fatalf("components holds %s", got)
	}
}

func TestUnpackReplacesTheOldInstall(t *testing.T) {
	archive := buildZip(t, goodEntries())
	spec := specFor(t, archive)
	s, data := newStore(t, "")
	installed := filepath.Join(data, componentsDir, installedDir)
	writeFile(t, filepath.Join(installed, "whisper.dll"), []byte("damaged"))
	writeFile(t, filepath.Join(installed, "stale.dll"), []byte("old"))
	save(t, s, archive)

	if err := s.Unpack(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if got := joined(names(t, installed)); got != "whisper-cli.exe,whisper.dll" {
		t.Fatalf("installed %s, want the new set only", got)
	}
	if got := joined(names(t, filepath.Join(data, componentsDir))); got != installedDir+","+archiveName {
		t.Fatalf("components holds %s: the old folder must be gone", got)
	}
}

// Entries outside the exact names never become files: zip slip is impossible by construction.
func TestUnpackIgnoresEveryOtherEntryName(t *testing.T) {
	entries := append(goodEntries(),
		zipEntry{name: "Release/../evil.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "../evil.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "../../evil.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "/evil.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "C:/evil.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: `Release\..\..\evil.dll`, data: []byte("evil"), method: zip.Store},
		zipEntry{name: "Release/sub/whisper.dll", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "Release/WHISPER.DLL", data: []byte("evil"), method: zip.Store},
		zipEntry{name: "release/whisper.dll", data: []byte("evil"), method: zip.Store},
	)
	archive := buildZip(t, entries)
	spec := specFor(t, archive)
	s, data := newStore(t, "")
	save(t, s, archive)

	if err := s.Unpack(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if got := joined(names(t, filepath.Join(data, componentsDir, installedDir))); got != "whisper-cli.exe,whisper.dll" {
		t.Fatalf("installed %s", got)
	}
	found, _ := s.Inspect(context.Background(), values.FolderInstalled, spec)
	if spec.Assess(found) != values.ComponentReady {
		t.Fatalf("installed set does not verify: %v", found)
	}
	err := filepath.WalkDir(filepath.Dir(data), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == "evil.dll" {
			t.Errorf("an entry escaped: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnpackRefusesBadArchives(t *testing.T) {
	good := goodEntries()
	replace := func(name string, e zipEntry) []zipEntry {
		out := append([]zipEntry(nil), good...)
		for i := range out {
			if out[i].name == name {
				out[i] = e
			}
		}
		return out
	}
	without := func(name string) []zipEntry {
		var out []zipEntry
		for _, e := range good {
			if e.name != name {
				out = append(out, e)
			}
		}
		return out
	}
	const dll = "Release/whisper.dll"
	longer := append(append([]byte(nil), dllData...), 'x')
	other := append([]byte(nil), dllData...)
	other[0] ^= 1

	tests := []struct {
		name    string
		entries []zipEntry
		want    error
	}{
		{"missing file", without(dll), values.ErrInvalidComponentArchive},
		{"repeated file", append(append([]zipEntry(nil), good...), zipEntry{name: dll, data: dllData}), values.ErrInvalidComponentArchive},
		{"directory under the name", replace(dll, zipEntry{name: dll, mode: fs.ModeDir | 0o755}), values.ErrInvalidComponentArchive},
		{"symlink under the name", replace(dll, zipEntry{name: dll, data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777}), values.ErrInvalidComponentArchive},
		{"encrypted", replace(dll, zipEntry{name: dll, data: dllData, flags: zipEncrypted}), values.ErrInvalidComponentArchive},
		{"declared size differs", replace(dll, zipEntry{name: dll, data: longer, method: zip.Deflate}), values.ErrSizeMismatch},
		{"declared size lies", replace(dll, zipEntry{name: dll, data: longer, declared: uint64(len(dllData))}), values.ErrInvalidComponentArchive},
		{"other content", replace(dll, zipEntry{name: dll, data: other, method: zip.Deflate}), values.ErrChecksumMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := buildZip(t, tt.entries)
			spec := specFor(t, archive)
			s, data := newStore(t, "")
			installed := filepath.Join(data, componentsDir, installedDir)
			writeFile(t, filepath.Join(installed, "keep.txt"), []byte("old install"))
			save(t, s, archive)

			if err := s.Unpack(context.Background(), spec); !errors.Is(err, tt.want) {
				t.Fatalf("Unpack = %v, want %v", err, tt.want)
			}
			// Partial results never become the installed set, and no staging folder is left.
			if got := joined(names(t, installed)); got != "keep.txt" {
				t.Fatalf("installed folder now holds %s", got)
			}
			if got := joined(names(t, filepath.Join(data, componentsDir))); got != installedDir+","+archiveName {
				t.Fatalf("components holds %s", got)
			}
		})
	}
}

// The stored archive is checked again before it is opened as a zip.
func TestUnpackChecksTheStoredArchive(t *testing.T) {
	archive := buildZip(t, goodEntries())
	spec := specFor(t, archive)
	notZip := make([]byte, len(archive))
	tests := []struct {
		name   string
		stored func(t *testing.T, s *Store, dir string)
		want   error
	}{
		{"none", func(*testing.T, *Store, string) {}, ErrStorage},
		{"shorter", func(t *testing.T, s *Store, _ string) { save(t, s, archive[:len(archive)-1]) }, values.ErrSizeMismatch},
		{"other bytes", func(t *testing.T, s *Store, _ string) {
			changed := append([]byte(nil), archive...)
			changed[len(changed)/2] ^= 1
			save(t, s, changed)
		}, values.ErrChecksumMismatch},
		{"a directory", func(t *testing.T, _ *Store, dir string) {
			if err := os.Mkdir(filepath.Join(dir, archiveName), 0o700); err != nil {
				t.Fatal(err)
			}
		}, values.ErrInvalidComponentArchive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, data := newStore(t, "")
			tt.stored(t, s, filepath.Join(data, componentsDir))
			if err := s.Unpack(context.Background(), spec); !errors.Is(err, tt.want) {
				t.Fatalf("Unpack = %v, want %v", err, tt.want)
			}
		})
	}

	// Pinned bytes that are not a zip at all.
	s, _ := newStore(t, "")
	save(t, s, notZip)
	if err := s.Unpack(context.Background(), specFor(t, notZip)); !errors.Is(err, values.ErrInvalidComponentArchive) {
		t.Fatalf("Unpack of zeros = %v, want ErrInvalidComponentArchive", err)
	}
}

func TestUnpackHonoursCancel(t *testing.T) {
	archive := buildZip(t, goodEntries())
	spec := specFor(t, archive)
	s, data := newStore(t, "")
	save(t, s, archive)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Unpack(ctx, spec); !errors.Is(err, context.Canceled) {
		t.Fatalf("Unpack = %v, want context.Canceled", err)
	}
	if got := joined(names(t, filepath.Join(data, componentsDir))); got != archiveName {
		t.Fatalf("components holds %s", got)
	}
}
