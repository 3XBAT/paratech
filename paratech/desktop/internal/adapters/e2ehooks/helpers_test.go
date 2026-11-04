package e2ehooks

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// run is a control dir with fixtures/clip.wav, fixtures/sub/notes.pdf, fixtures/sub/photo.jpg and, outside the
// fixtures, outside/secret.wav.
type run struct {
	dir     string
	outside string
	logs    *syncBuffer
	h       *Hooks
}

func newRun(t *testing.T) run {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{"fixtures/clip.wav", "fixtures/sub/notes.pdf", "fixtures/sub/photo.jpg", "outside/secret.wav"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), "data of "+p)
	}
	logs := &syncBuffer{}
	h := New(dir, slog.New(slog.NewTextHandler(logs, nil)))
	h.now = func() time.Time { return time.Date(2026, 11, 4, 10, 30, 0, 0, time.UTC) }
	return run{dir: dir, outside: filepath.Join(dir, "outside"), logs: logs, h: h}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// queue writes picks.jsonl with lines, each ended by '\n'.
func (r run) queue(t *testing.T, lines ...string) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	writeFile(t, filepath.Join(r.dir, picksFile), b.String())
}

// fixture is the resolved path of a file below fixtures.
func (r run) fixture(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(filepath.Join(r.dir, fixturesDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (r run) consumed(t *testing.T) []consumedLine {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, consumedFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []consumedLine
	for _, l := range bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n")) {
		var c consumedLine
		if err := json.Unmarshal(l, &c); err != nil {
			t.Fatalf("consumed line %q: %v", l, err)
		}
		out = append(out, c)
	}
	return out
}

func (r run) opened(t *testing.T) []openedLine {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, openedFile))
	if err != nil {
		t.Fatal(err)
	}
	var out []openedLine
	for _, l := range bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n")) {
		var o openedLine
		if err := json.Unmarshal(l, &o); err != nil {
			t.Fatalf("opened line %q: %v", l, err)
		}
		out = append(out, o)
	}
	return out
}

// pickJSON is a picks.jsonl line.
func pickJSON(t *testing.T, dialog string, paths ...string) string {
	t.Helper()
	b, err := json.Marshal(pickLine{Dialog: dialog, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
