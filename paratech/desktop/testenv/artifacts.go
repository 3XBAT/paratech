package testenv

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"paratech/desktop/internal/domain/shared"
)

// MemArtifacts is an in-memory Storage Manager with the semantics of the filestore adapter: files by reference,
// directories implied by their files, List of direct children sorted by name, idempotent recursive Delete.
type MemArtifacts struct {
	mu       sync.Mutex
	files    map[string][]byte
	writeErr error
}

// NewMemArtifacts returns an empty store.
func NewMemArtifacts() *MemArtifacts { return &MemArtifacts{files: map[string][]byte{}} }

// FailWrites makes every later Write fail with err (for example shared.ErrStorageFull); nil heals the store.
func (m *MemArtifacts) FailWrites(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = err
}

// Write stores the content of a file.
func (m *MemArtifacts) Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return fmt.Errorf("write %s: %w", ref, m.writeErr)
	}
	if m.isDir(ref.String()) {
		return fmt.Errorf("%w: %s is a directory", shared.ErrConflict, ref)
	}
	m.files[ref.String()] = data
	return nil
}

// Open returns a reader of a file.
func (m *MemArtifacts) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[ref.String()]
	if !ok {
		return nil, fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// List returns the direct children of a directory sorted by name; a missing directory yields shared.ErrNotFound.
func (m *MemArtifacts) List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, isFile := m.files[dir.String()]; isFile {
		return nil, fmt.Errorf("%w: %s is not a directory", shared.ErrConflict, dir)
	}

	prefix := dir.String() + "/"
	seen := map[string]bool{}
	for k := range m.files {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			name, _, _ := strings.Cut(rest, "/")
			seen[name] = true
		}
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%w: %s", shared.ErrNotFound, dir)
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]shared.ArtifactRef, 0, len(names))
	for _, n := range names {
		child, err := dir.Child(n)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, nil
}

// Delete removes a file or a directory tree; a missing target is not an error.
func (m *MemArtifacts) Delete(ctx context.Context, ref shared.ArtifactRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ref.String()
	for k := range m.files {
		if k == key || strings.HasPrefix(k, key+"/") {
			delete(m.files, k)
		}
	}
	return nil
}

// Paths returns the stored files, sorted.
func (m *MemArtifacts) Paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for k := range m.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// isDir reports whether files live below key; m.mu must be held.
func (m *MemArtifacts) isDir(key string) bool {
	for k := range m.files {
		if strings.HasPrefix(k, key+"/") {
			return true
		}
	}
	return false
}
