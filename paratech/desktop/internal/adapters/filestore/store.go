package filestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/domain/shared"
)

const (
	dirPerm  = 0o700
	filePerm = 0o600

	// tmpPrefix marks temporary files of Write; references with such a segment are rejected.
	tmpPrefix = ".filestore-tmp-"

	retryAttempts = 6
	retryStep     = 15 * time.Millisecond
)

// Operation names used in error messages.
const (
	opOpen       = "open"
	opSize       = "size"
	opPath       = "path"
	opList       = "list"
	opDelete     = "delete"
	opWrite      = "write"
	opCleanup    = "cleanup"
	opCreateRoot = "create root"
	opOpenRoot   = "open root"
	opCreateDir  = "create"
)

var kinds = []shared.ArtifactKind{
	shared.ArtifactRecordings, shared.ArtifactTranscripts, shared.ArtifactModels, shared.ArtifactExports,
	shared.ArtifactMaterials,
}

// errNotDir means a non-final path component exists but is not a directory.
var errNotDir = errors.New("not a directory")

// Store keeps artifacts below a private data root. It is safe for concurrent use.
type Store struct {
	root *os.Root

	// beforeRename is a test hook called after the temporary file is closed and before the rename.
	beforeRename func()

	// afterMkdir is a test hook called after the directory of a write is ensured, before the temp file is created.
	afterMkdir func()

	// wrote is set by the first Write; CleanupTemp refuses to run after it.
	wrote atomic.Bool
}

// New opens the data root (an absolute path), creating it and the per-kind directories if needed.
func New(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: the path must be absolute", ErrInvalidRoot)
	}

	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, storageFail(opCreateRoot, err)
	}

	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, storageFail(opOpenRoot, err)
	}

	for _, k := range kinds {
		name := string(k)

		// Lstat first: Mkdir on a pre-planted junction can create a directory outside the root (GO-2026-6604).
		if _, err := r.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			if err := r.Mkdir(name, dirPerm); err != nil && !errors.Is(err, fs.ErrExist) {
				_ = r.Close()
				return nil, storageFail(opCreateDir+" "+name, err)
			}
		}

		fi, err := r.Lstat(name)
		if err != nil || !fi.IsDir() {
			_ = r.Close()
			return nil, fmt.Errorf("%w: %s is not a plain directory", ErrInvalidRoot, name)
		}
	}

	return &Store{root: r}, nil
}

// Close releases the root handle.
func (s *Store) Close() error { return s.root.Close() }

// Open returns a reader of the file; a missing file yields shared.ErrNotFound.
func (s *Store) Open(ctx context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	rel, fi, err := s.lookup(ctx, opOpen, ref, false)
	if err != nil {
		return nil, err
	}

	if fi.IsDir() {
		return nil, isDirErr(ref)
	}

	f, err := s.root.Open(rel)
	if err != nil {
		return nil, mapErr(opOpen, ref, err)
	}

	return f, nil
}

// OpenSeekable returns a seekable reader of a regular file and its size, both from the open handle, for serving a
// recording with byte ranges (TASK-079). A missing file yields shared.ErrNotFound; the caller closes the reader.
func (s *Store) OpenSeekable(ctx context.Context, ref shared.ArtifactRef) (io.ReadSeekCloser, int64, error) {
	rel, fi, err := s.lookup(ctx, opOpen, ref, false)
	if err != nil {
		return nil, 0, err
	}
	if fi.IsDir() {
		return nil, 0, isDirErr(ref)
	}

	f, err := s.root.Open(rel)
	if err != nil {
		return nil, 0, mapErr(opOpen, ref, err)
	}
	// Checked again on the handle: the file may have been replaced since the lookup.
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			return nil, 0, isDirErr(ref)
		}
		return nil, 0, mapErr(opOpen, ref, err)
	}

	return f, st.Size(), nil
}

// Size returns the size of the file in bytes.
func (s *Store) Size(ctx context.Context, ref shared.ArtifactRef) (int64, error) {
	_, fi, err := s.lookup(ctx, opSize, ref, false)
	if err != nil {
		return 0, err
	}

	if fi.IsDir() {
		return 0, isDirErr(ref)
	}

	return fi.Size(), nil
}

// Path returns the absolute OS path of an existing file, for a sidecar process that needs a path (the Paths port of
// the whispercli adapter). Every component is checked for links as in Open; a missing file yields shared.ErrNotFound
// and a directory shared.ErrConflict. The file can change after the check, so the consumer checks the path again
// before it hands it to a process.
func (s *Store) Path(ctx context.Context, ref shared.ArtifactRef) (string, error) {
	rel, fi, err := s.lookup(ctx, opPath, ref, false)
	if err != nil {
		return "", err
	}

	if fi.IsDir() {
		return "", isDirErr(ref)
	}

	return filepath.Join(s.root.Name(), rel), nil
}

// List returns the direct children of a directory sorted by name. Temporary files, links and entries that
// cannot be references are skipped.
func (s *Store) List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error) {
	rel, fi, err := s.lookup(ctx, opList, dir, false)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", shared.ErrConflict, dir)
	}

	d, err := s.root.Open(rel)
	if err != nil {
		return nil, mapErr(opList, dir, err)
	}

	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return nil, mapErr(opList, dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) || e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		names = append(names, e.Name())
	}

	sort.Strings(names)

	out := make([]shared.ArtifactRef, 0, len(names))
	for _, n := range names {
		if child, err := dir.Child(n); err == nil {
			out = append(out, child)
		}
	}

	return out, nil
}

// Delete removes a file or a whole directory tree. A missing target is not an error. If the target itself is a
// link, only the link is removed.
func (s *Store) Delete(ctx context.Context, ref shared.ArtifactRef) error {
	rel, _, err := s.lookup(ctx, opDelete, ref, true)
	if errors.Is(err, shared.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	return mapErr(opDelete, ref, retry(ctx, func() error { return s.root.RemoveAll(rel) }))
}

// Write stores the content atomically: temporary file in the same directory, fsync, rename over the target.
// A failed or interrupted write leaves the previous content untouched. It never returns shared.ErrNotFound: a
// directory removed concurrently is recreated once, and a later loss is shared.ErrStorageBusy. On Windows close
// readers of the target before overwriting it; an open file blocks the rename (ErrStorageBusy after a few retries).
func (s *Store) Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.wrote.Store(true)

	parts, err := locate(ref)
	if err != nil {
		return err
	}

	fi, err := s.inspect(parts, false)
	switch {
	case errors.Is(err, errNotDir):
		return fmt.Errorf("%w: a parent of %s is a file", shared.ErrConflict, ref)
	case err != nil:
		return mapErr(opWrite, ref, err)
	case fi != nil && fi.IsDir():
		return isDirErr(ref)
	}

	rel := filepath.Join(parts...)
	dir := filepath.Dir(rel)

	var tmp string
	var f *os.File
	// The directory may vanish between MkdirAll and the create (concurrent Delete); try once more.
	for attempt := 0; ; attempt++ {
		if err := s.root.MkdirAll(dir, dirPerm); err != nil {
			return writeErr(ref, err)
		}

		if tmp, err = tmpName(dir); err != nil {
			return writeErr(ref, err)
		}

		if s.afterMkdir != nil {
			s.afterMkdir()
		}

		f, err = s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
		if err == nil {
			break
		}

		if attempt > 0 || !errors.Is(err, fs.ErrNotExist) {
			return writeErr(ref, err)
		}
	}

	// Cleanup is explicit, not deferred: a panic (a simulated crash in tests) must leave the temp file behind.
	src := &errTrackReader{ctx: ctx, r: content}

	_, err = io.Copy(f, src)
	if err == nil {
		err = f.Sync()
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		_ = s.root.Remove(tmp)
		if src.err != nil && errors.Is(err, src.err) {
			return fmt.Errorf("read content of %s: %w", ref, err)
		}
		return writeErr(ref, err)
	}

	if s.beforeRename != nil {
		s.beforeRename()
	}

	// The content is consumed, so a vanished directory (and with it the temp file) cannot be redone here.
	if err := retry(ctx, func() error { return s.root.Rename(tmp, rel) }); err != nil {
		_ = s.root.Remove(tmp)
		return writeErr(ref, err)
	}
	syncDir(s.root, dir)

	return nil
}

// writeErr maps an error of Write; a missing path is a concurrent change, not "artifact not found".
func writeErr(ref shared.ArtifactRef, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &storageError{
			msg:   fmt.Sprintf("%v: write %s: directory removed concurrently", ErrStorage, ref),
			kinds: []error{ErrStorage, shared.ErrStorageBusy},
			cause: bareErr(err),
		}
	}
	return mapErr(opWrite, ref, err)
}

// CleanupTemp removes temporary files left by crashed writes and returns how many it removed. Call it once at
// startup, before any Write: it fails with shared.ErrConflict if this Store has already written. It keeps going
// after an error and returns the first one; on a cancelled context it stops and returns the context error.
func (s *Store) CleanupTemp(ctx context.Context) (int, error) {
	if s.wrote.Load() {
		return 0, fmt.Errorf("%w: CleanupTemp must run before any Write", shared.ErrConflict)
	}

	n := 0
	var first error
	note := func(err error) {
		if first == nil {
			first = storageFail(opCleanup, err)
		}
	}

	err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}

		if err != nil {
			note(err)
			return nil
		}

		if d.Type().IsRegular() && strings.HasPrefix(d.Name(), tmpPrefix) {
			switch err := s.root.Remove(filepath.FromSlash(p)); {
			case err == nil:
				n++
			case !errors.Is(err, fs.ErrNotExist):
				note(err)
			}
		}

		return nil
	})
	if err != nil {
		return n, err
	}

	return n, first
}

// lookup resolves ref and inspects it. A missing target (or a file where a directory is expected) is ErrNotFound.
func (s *Store) lookup(ctx context.Context, op string, ref shared.ArtifactRef, allowFinalLink bool) (string, fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	parts, err := locate(ref)
	if err != nil {
		return "", nil, err
	}

	fi, err := s.inspect(parts, allowFinalLink)
	if errors.Is(err, errNotDir) || (err == nil && fi == nil) {
		return "", nil, fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	if err != nil {
		return "", nil, mapErr(op, ref, err)
	}

	return filepath.Join(parts...), fi, nil
}

// locate turns a reference into path components below the root and re-validates them.
func locate(ref shared.ArtifactRef) ([]string, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("%w: zero reference", shared.ErrInvalidArtifactRef)
	}

	// ArtifactRef is already validated; this is defense in depth against a future change of its rules.
	if _, err := shared.NewArtifactRef(ref.Kind(), ref.Path()); err != nil {
		return nil, err
	}

	parts := append([]string{string(ref.Kind())}, strings.Split(ref.Path(), "/")...)
	for _, p := range parts {
		if strings.HasPrefix(p, tmpPrefix) {
			return nil, fmt.Errorf("%w: %s uses the reserved temporary prefix", shared.ErrInvalidArtifactRef, ref)
		}
	}

	if !filepath.IsLocal(filepath.Join(parts...)) {
		return nil, fmt.Errorf("%w: %s leaves the root", shared.ErrInvalidArtifactRef, ref)
	}

	return parts, nil
}

// inspect walks the components with Lstat and refuses links. It returns nil info when the target does not exist and
// errNotDir when a non-final component is not a directory. A link as the final component is allowed on request.
func (s *Store) inspect(parts []string, allowFinalLink bool) (fs.FileInfo, error) {
	var fi fs.FileInfo
	for i := range parts {
		var err error

		fi, err = s.root.Lstat(filepath.Join(parts[:i+1]...))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		last := i == len(parts)-1

		if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if last && allowFinalLink {
				return fi, nil
			}
			return nil, fmt.Errorf("%w: %s is a link", shared.ErrInvalidArtifactRef, path.Join(parts[:i+1]...))
		}

		if !last && !fi.IsDir() {
			return nil, errNotDir
		}
	}

	return fi, nil
}

func isDirErr(ref shared.ArtifactRef) error {
	return fmt.Errorf("%w: %s is a directory", shared.ErrConflict, ref)
}

func tmpName(dir string) (string, error) {
	var b [8]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return filepath.Join(dir, tmpPrefix+hex.EncodeToString(b[:])), nil
}

// retry repeats fn a few times: on Windows a rename or delete fails while a reader (antivirus, indexer) holds the file.
func retry(ctx context.Context, fn func() error) error {
	var err error
	for i := 0; i < retryAttempts; i++ {
		if err = fn(); err == nil || errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if i == retryAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i+1) * retryStep):
		}
	}
	return err
}

// errTrackReader stops on a cancelled context and remembers the error of the caller's reader.
type errTrackReader struct {
	ctx context.Context
	r   io.Reader
	err error
}

func (t *errTrackReader) Read(p []byte) (int, error) {
	if err := t.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := t.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		t.err = err
	}
	return n, err
}
