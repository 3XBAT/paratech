// Package visitfile keeps the time the student last opened the page of each subject (TASK-225) in one small JSON
// file of the data directory: {"subjects":{"<subject id>":<unix milliseconds>}}. It drives only the «новое» marks,
// so it lives outside the database and needs no migration. The file is replaced atomically (a temporary file renamed
// over it); one Store serves the one process the instance lock allows. Standard library only.
//
// Errors never contain the path (it lies below the user's profile).
package visitfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors.
const (
	opRead   = "read the file"
	opDecode = "decode the file"
	opCreate = "create the file"
	opWrite  = "write the file"
	opFinish = "finish the file"
	opRename = "rename the file"
)

// Limits of the file. Subjects are created by hand, so MaxEntries is far above any real use; past it the oldest
// openings are dropped.
const (
	MaxEntries = 1000
	maxBytes   = 1 << 20
)

var (
	// ErrCorrupt means the file is not what this package writes (damaged or too large); the next MarkOpened replaces
	// it.
	ErrCorrupt = errors.New("visitfile: the file is damaged")

	// ErrIO means the file cannot be read or written; the system error is wrapped without its path.
	ErrIO = errors.New("visitfile: the file cannot be read or written")

	// ErrInvalid means a malformed subject id or a zero time.
	ErrInvalid = errors.New("visitfile: invalid request")
)

// document is the content of the file.
type document struct {
	Subjects map[string]int64 `json:"subjects"`
}

// Store reads and writes the file at path. Create it with New.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns the store of the file at path (absolute; the file may not exist yet).
func New(path string) *Store { return &Store{path: filepath.Clean(path)} }

// LastOpened returns the last opening of subject id; zero when none is recorded or the file does not exist.
func (s *Store) LastOpened(ctx context.Context, id shared.SubjectID) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	if id.Validate() != nil {
		return time.Time{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.load()
	if err != nil {
		return time.Time{}, err
	}
	ms, ok := doc.Subjects[string(id)]
	if !ok {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms), nil
}

// MarkOpened records an opening of subject id at at. A damaged file is replaced by one holding only this opening.
func (s *Store) MarkOpened(ctx context.Context, id shared.SubjectID, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id.Validate() != nil || at.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.load()
	switch {
	case errors.Is(err, ErrCorrupt):
		doc = document{Subjects: map[string]int64{}}
	case err != nil:
		return err
	}
	doc.Subjects[string(id)] = at.UnixMilli()
	trim(doc.Subjects)
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrIO, opWrite, err)
	}
	return s.replace(data)
}

// trim drops the oldest openings past MaxEntries.
func trim(m map[string]int64) {
	for len(m) > MaxEntries {
		oldest, first := "", true
		for k, v := range m {
			if first || v < m[oldest] || (v == m[oldest] && k < oldest) {
				oldest, first = k, false
			}
		}
		delete(m, oldest)
	}
}

// load reads the file; a missing one is empty.
func (s *Store) load() (document, error) {
	doc := document{Subjects: map[string]int64{}}
	f, err := os.Open(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return doc, nil
	case err != nil:
		return document{}, fail(opRead, err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	switch {
	case err != nil:
		return document{}, fail(opRead, err)
	case len(data) > maxBytes:
		return document{}, ErrCorrupt
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("%w: %s", ErrCorrupt, opDecode)
	}
	if doc.Subjects == nil {
		doc.Subjects = map[string]int64{}
	}
	return doc, nil
}

// replace writes data to a temporary file next to the target and renames it over the target.
func (s *Store) replace(data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".subject-visits-*.tmp")
	if err != nil {
		return fail(opCreate, err)
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fail(opWrite, err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(opFinish, err)
	}
	if err := tmp.Close(); err != nil {
		return fail(opFinish, err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fail(opRename, err)
	}
	done = true
	return nil
}

// fail is ErrIO with the operation and the system error without its path.
func fail(op string, err error) error {
	var (
		pe *fs.PathError
		le *os.LinkError
	)
	switch {
	case errors.As(err, &pe):
		err = pe.Err
	case errors.As(err, &le):
		err = le.Err
	}
	return fmt.Errorf("%w: %s: %w", ErrIO, op, err)
}
