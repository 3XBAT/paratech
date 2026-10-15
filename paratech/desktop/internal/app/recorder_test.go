package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/adapters/filestore"
	"paratech/desktop/testenv"
)

// lockedBuffer is the child's stdout in the test.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func recorderApp(t *testing.T, dataDir string, src audiocapture.Source) (*App, *io.PipeWriter, *lockedBuffer) {
	t.Helper()
	cfg := config.Default()
	cfg.Recorder, cfg.DataDir = true, dataDir // not headless: the recorder mode never builds the UI shell
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if a.shell != nil {
		t.Fatal("the recorder mode built the UI shell")
	}
	pr, pw := io.Pipe()
	out := &lockedBuffer{}
	a.env.childStdin, a.env.childStdout, a.env.captureSource = pr, out, src
	t.Cleanup(func() { _ = pw.Close(); _ = a.Close() })
	return a, pw, out
}

func TestRecorderModeCapturesIntoTheArtifactStore(t *testing.T) {
	dataDir := t.TempDir()
	src := &testenv.ToneSource{Format: testenv.FakeRecorderFormat(), PacketFrames: 800, Limit: 10}
	a, stdin, out := recorderApp(t, dataDir, src)

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background()) }()
	go func() {
		_, _ = stdin.Write([]byte(`{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"system","pid":0},"chunk_seconds":30,"lead_silence_ms":0}` + "\n"))
	}()
	partial := filepath.Join(dataDir, artifactsDir, "recordings", "rec1", "chunk-000001.wav.partial")
	testenv.Eventually(t, "the first chunk grows", func() bool { _, err := os.Stat(partial); return err == nil }, nil)
	_ = stdin.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the recorder did not stop when stdin closed")
	}
	if !strings.Contains(out.String(), `"type":"stopped"`) {
		t.Fatalf("events = %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, artifactsDir, "recordings", "rec1", "chunk-000001.wav")); err != nil {
		t.Fatalf("no chunk in the artifact store: %v", err)
	}
	// Only the Storage Manager is opened: no database, no instance lock.
	for _, name := range []string{dbFile, "learning-assistant.lock"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists in the recorder mode: %v", name, err)
		}
	}
}

func TestRecorderModeFailsWithoutCapture(t *testing.T) {
	// The platform source of a build without capture; where the build captures, a stand-in that fails the same way,
	// so the test never touches the audio devices.
	src := audiocapture.PlatformSource()
	if audiocapture.Supported() {
		src = &testenv.ToneSource{OpenErr: audiocapture.ErrUnavailable}
	}
	a, stdin, out := recorderApp(t, t.TempDir(), src)
	go func() {
		_, _ = stdin.Write([]byte(`{"v":1,"recording":"rec1","chunks":"recordings/rec1","target":{"mode":"system","pid":0},"chunk_seconds":30,"lead_silence_ms":0}` + "\n"))
	}()
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("Run without a capture source must fail")
	}
	if !strings.Contains(out.String(), `"unavailable":true`) {
		t.Fatalf("events = %s", out.String())
	}
}

// The production recorder of this build: the recorder child on Windows, Unavailable elsewhere. Building it starts no
// child.
func TestPlatformRecorder(t *testing.T) {
	dataDir := t.TempDir()
	store, err := filestore.New(filepath.Join(dataDir, artifactsDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	rec, err := productionEnv().recorder(dataDir, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	switch rec.(type) {
	case *audiocapture.Recorder:
		if !audiocapture.Supported() {
			t.Fatal("a build without capture got the recorder child")
		}
	case audiocapture.Unavailable:
		if audiocapture.Supported() {
			t.Fatal("a build with capture got the unavailable recorder")
		}
	default:
		t.Fatalf("recorder %T", rec)
	}
	if _, err := productionEnv().recorder("relative", store, slog.New(slog.DiscardHandler)); audiocapture.Supported() && err == nil {
		t.Fatal("the recorder child needs an absolute data directory")
	}
}
