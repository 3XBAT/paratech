package componentstore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

// component files of the tests.
var (
	exeData = []byte("the program")
	dllData = []byte("the library, a little longer")
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// zipEntry is one entry of a generated archive. declared, when not zero, is the size the header claims (the data is
// stored raw, so the claim can lie).
type zipEntry struct {
	name     string
	data     []byte
	mode     fs.FileMode
	method   uint16
	flags    uint16
	declared uint64
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method, Flags: e.flags}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		if e.declared != 0 {
			h.Method = zip.Store
			h.CRC32 = crc32.ChecksumIEEE(e.data)
			h.CompressedSize64 = uint64(len(e.data))
			h.UncompressedSize64 = e.declared
			fw, err := w.CreateRaw(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(e.data); err != nil {
				t.Fatal(err)
			}
			continue
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// goodEntries is the component as the release ships it, with extra entries the install must ignore.
func goodEntries() []zipEntry {
	return []zipEntry{
		{name: "Release/", mode: fs.ModeDir | 0o755},
		{name: "Release/whisper-cli.exe", data: exeData, method: zip.Deflate},
		{name: "Release/whisper.dll", data: dllData, method: zip.Store},
		{name: "Release/main.exe", data: []byte("not needed"), method: zip.Deflate},
		{name: "README.md", data: []byte("readme"), method: zip.Deflate},
	}
}

// specFor is the manifest of exeData and dllData whose archive is archive.
func specFor(t *testing.T, archive []byte) values.SpeechComponent {
	t.Helper()
	doc := fmt.Sprintf(`{"schema_version": 1, "version": "b1", "source_url": "https://example.com/r/a.zip",
 "size_bytes": %d, "sha256": %q, "download_hosts": [], "entry_prefix": "Release/", "executable": "whisper-cli.exe",
 "files": [{"name": "whisper-cli.exe", "size_bytes": %d, "sha256": %q},
           {"name": "whisper.dll", "size_bytes": %d, "sha256": %q}]}`,
		len(archive), sum(archive), len(exeData), sum(exeData), len(dllData), sum(dllData))
	spec, err := values.ParseSpeechComponent([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// newStore is a Store on a fresh data directory; bundled is "" or a folder.
func newStore(t *testing.T, bundled string) (*Store, string) {
	t.Helper()
	data := t.TempDir()
	s, err := New(data, bundled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, data
}

// save stores archive as the downloaded archive.
func save(t *testing.T, s *Store, archive []byte) {
	t.Helper()
	if _, _, err := s.SaveArchive(context.Background(), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
}

// names lists a directory, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func joined(list []string) string { return strings.Join(list, ",") }
