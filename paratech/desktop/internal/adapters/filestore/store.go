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

var kinds = []shared.ArtifactKind{
	shared.ArtifactRecordings, shared.ArtifactTranscripts, shared.ArtifactModels, shared.ArtifactExports,
}

// errNotDir means a non-final path component exists but is not a directory.
var errNotDir = errors.New("not a directory")

// Store keeps artifacts below a private data root. It is safe for concurrent use.
type Store struct {
	root *os.Root

	// beforeRename is a test hook called after the temporary file is closed and before the rename.
	beforeRename func()
}

// New opens the data root (an absolute path), creating it and the per-kind directories if needed.
func New(root string) (*Store, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: the path must be absolute", ErrInvalidRoot)
	}
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return nil, fmt.Errorf("%w: create root: %s", ErrStorage, reason(err))
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("%w: open root: %s", ErrStorage, reason(err))
	}
	for _, k := range kinds {
		name := string(k)
		if err := r.Mkdir(name, dirPerm); err != nil && !errors.Is(err, fs.ErrExist) {
			_ = r.Close()
			return nil, fmt.Errorf("%w: create %s: %s", ErrStorage, name, reason(err))
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
	rel, fi, err := s.lookup(ctx, "open", ref, false)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, isDirErr(ref)
	}
	f, err := s.root.Open(rel)
	if err != nil {
		return nil, mapErr("open", ref, err)
	}
	return f, nil
}

// Size returns the size of the file in bytes.
func (s *Store) Size(ctx context.Context, ref shared.ArtifactRef) (int64, error) {
	_, fi, err := s.lookup(ctx, "size", ref, false)
	if err != nil {
		return 0, err
	}
	if fi.IsDir() {
		return 0, isDirErr(ref)
	}
	return fi.Size(), nil
}

// List returns the direct children of a directory sorted by name. Temporary files, links and entries that
// cannot be references are skipped.
func (s *Store) List(ctx context.Context, dir shared.ArtifactRef) ([]shared.ArtifactRef, error) {
	rel, fi, err := s.lookup(ctx, "list", dir, false)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", shared.ErrConflict, dir)
	}
	d, err := s.root.Open(rel)
	if err != nil {
		return nil, mapErr("list", dir, err)
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return nil, mapErr("list", dir, err)
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
	rel, _, err := s.lookup(ctx, "delete", ref, true)
	if errors.Is(err, shared.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return mapErr("delete", ref, retry(ctx, func() error { return s.root.RemoveAll(rel) }))
}

// Write stores the content atomically: temporary file in the same directory, fsync, rename over the target.
// A failed or interrupted write leaves the previous content untouched.
func (s *Store) Write(ctx context.Context, ref shared.ArtifactRef, content io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parts, err := locate(ref)
	if err != nil {
		return err
	}
	fi, err := s.inspect(parts, false)
	switch {
	case errors.Is(err, errNotDir):
		return fmt.Errorf("%w: a parent of %s is a file", shared.ErrConflict, ref)
	case err != nil:
		return mapErr("write", ref, err)
	case fi != nil && fi.IsDir():
		return isDirErr(ref)
	}
	rel := filepath.Join(parts...)
	dir := filepath.Dir(rel)
	if err := s.root.MkdirAll(dir, dirPerm); err != nil {
		return mapErr("write", ref, err)
	}

	tmp, err := tmpName(dir)
	if err != nil {
		return mapErr("write", ref, err)
	}
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return mapErr("write", ref, err)
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
		return mapErr("write", ref, err)
	}
	if s.beforeRename != nil {
		s.beforeRename()
	}
	if err := retry(ctx, func() error { return s.root.Rename(tmp, rel) }); err != nil {
		_ = s.root.Remove(tmp)
		return mapErr("write", ref, err)
	}
	syncDir(s.root, dir)
	return nil
}

// CleanupTemp removes temporary files left by crashed writes and returns how many were removed. Call it once at
// startup, before any Write.
func (s *Store) CleanupTemp() (int, error) {
	n := 0
	err := fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() && strings.HasPrefix(d.Name(), tmpPrefix) {
			if err := s.root.Remove(filepath.FromSlash(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return n, fmt.Errorf("%w: cleanup: %s", ErrStorage, reason(err))
	}
	return n, nil
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
