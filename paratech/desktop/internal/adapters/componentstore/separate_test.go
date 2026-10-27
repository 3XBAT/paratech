package componentstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

var vadData = []byte("the voice activity model")

// specWithVAD is specFor with the VAD model downloaded on its own.
func specWithVAD(t *testing.T, archive []byte) values.SpeechComponent {
	t.Helper()
	doc := fmt.Sprintf(`{"schema_version": 1, "version": "b1", "source_url": "https://example.com/r/a.zip",
 "size_bytes": %d, "sha256": %q, "download_hosts": [], "entry_prefix": "Release/", "executable": "whisper-cli.exe",
 "vad_model": "vad.bin",
 "files": [{"name": "whisper-cli.exe", "size_bytes": %d, "sha256": %q},
           {"name": "whisper.dll", "size_bytes": %d, "sha256": %q},
           {"name": "vad.bin", "size_bytes": %d, "sha256": %q, "source_url": "https://example.org/vad.bin"}]}`,
		len(archive), sum(archive), len(exeData), sum(exeData), len(dllData), sum(dllData), len(vadData), sum(vadData))
	spec, err := values.ParseSpeechComponent([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func saveVAD(t *testing.T, s *Store, spec values.SpeechComponent, data []byte) {
	t.Helper()
	f, _ := spec.File("vad.bin")
	n, got, err := s.SaveFile(context.Background(), f, bytes.NewReader(data))
	if err != nil || n != int64(len(data)) || got != values.SHA256(sum(data)) {
		t.Fatalf("SaveFile = %d, %s, %v", n, got, err)
	}
}

func TestUnpackInstallsTheSeparateFiles(t *testing.T) {
	ctx := context.Background()
	archive := buildZip(t, goodEntries())
	spec := specWithVAD(t, archive)
	s, data := newStore(t, "")
	save(t, s, archive)
	saveVAD(t, s, spec, vadData)

	if err := s.Unpack(ctx, spec); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(data, componentsDir, installedDir)
	if got := joined(names(t, installed)); got != "vad.bin,whisper-cli.exe,whisper.dll" {
		t.Fatalf("installed %s, want the archive files and the VAD model", got)
	}
	found, err := s.Inspect(ctx, values.FolderInstalled, spec)
	if err != nil || spec.Assess(found) != values.ComponentReady {
		t.Fatalf("Inspect = %v, %v; want the verified set", found, err)
	}

	if err := s.DiscardArchive(ctx); err != nil {
		t.Fatal(err)
	}
	if got := joined(names(t, filepath.Join(data, componentsDir))); got != installedDir {
		t.Fatalf("components holds %s after DiscardArchive, want only the installed folder", got)
	}
}

func TestUnpackRefusesABadSeparateFile(t *testing.T) {
	archive := buildZip(t, goodEntries())
	tests := []struct {
		name  string
		store []byte // nil: nothing stored
		want  error
	}{
		{"not stored", nil, ErrStorage},
		{"other content", []byte("the voice activity modeX"), values.ErrChecksumMismatch},
		{"too long", append(append([]byte(nil), vadData...), 'x'), values.ErrSizeMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := specWithVAD(t, archive)
			s, data := newStore(t, "")
			installed := filepath.Join(data, componentsDir, installedDir)
			writeFile(t, filepath.Join(installed, "whisper.dll"), dllData)
			save(t, s, archive)
			if tt.store != nil {
				saveVAD(t, s, spec, tt.store)
			}

			if err := s.Unpack(context.Background(), spec); !errors.Is(err, tt.want) {
				t.Fatalf("Unpack = %v, want %v", err, tt.want)
			}
			if got := joined(names(t, installed)); got != "whisper.dll" {
				t.Fatalf("installed %s: a failed unpack must leave the old folder", got)
			}
			for _, n := range names(t, filepath.Join(data, componentsDir)) {
				if hasPrefix(n, stagingPrefix) {
					t.Fatalf("staging folder %s left behind", n)
				}
			}
		})
	}
}

func TestCleanupRemovesStoredSeparateFiles(t *testing.T) {
	s, data := newStore(t, "")
	dir := filepath.Join(data, componentsDir)
	writeFile(t, filepath.Join(dir, installedDir, "vad.bin"), vadData)
	writeFile(t, filepath.Join(dir, downloadPrefix+"vad.bin"), vadData)

	n, err := s.Cleanup(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Cleanup = %d, %v; want 1 removed", n, err)
	}
	if got := joined(names(t, dir)); got != installedDir {
		t.Fatalf("left %s", got)
	}
}
