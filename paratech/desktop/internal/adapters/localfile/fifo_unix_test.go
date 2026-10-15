//go:build unix

package localfile

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"

	"paratech/desktop/internal/domain/transcription/values"
)

// TestOpenRefusesPipe: a named pipe is refused before the open, which would block until a writer comes.
func TestOpenRefusesPipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New().Open(context.Background(), path); !errors.Is(err, values.ErrInvalidImportFile) {
		t.Fatalf("Open(fifo) = %v, want values.ErrInvalidImportFile", err)
	}
}
