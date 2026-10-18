// Package componentstore keeps the speech recognition component on disk (TASK-072): it reads the folder of the
// executable (what the installer ships, read only) and owns <data dir>/components, where the in-app install stores
// the downloaded archive, unpacks it into a staging folder and swaps the verified set in as components/speech. It
// satisfies the ComponentFiles port of cases/speechcomponent structurally. Standard library only.
//
// Trust boundary: the archive comes from the network. Unpack opens it only when it is a regular file with the pinned
// size and SHA-256 (read into memory, so it cannot change between the check and the use), takes only entries named
// exactly <prefix><component file>, writes each to a path built from the manifest name, never from the entry name,
// bounds and hashes every file while writing, and replaces the installed folder only when every file verified.
// Nothing from the archive is executed here. Links and reparse points are never followed: a linked folder or file
// does not count as the component.
//
// Errors carry no absolute path: the data directory lies below the user's profile.
package componentstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// Layout of <data dir>/components.
const (
	componentsDir = "components"
	installedDir  = "speech"
	archiveName   = "speech.zip"
	tmpPrefix     = "tmp-"
	stagingPrefix = "staging-"
	retiredPrefix = "retired-"

	dirPerm = 0o700
	// filePerm keeps the program executable where modes count; Windows ignores it (the data directory ACL applies).
	filePerm = 0o700
)

// Windows holds a fresh executable open for a moment (antivirus, indexer): a rename or a removal is retried.
const (
	retryAttempts = 8
	retryStep     = 50 * time.Millisecond
)

// Operation names used in error messages.
const (
	opOpen    = "open the component folder"
	opCreate  = "create the component folder"
	opInspect = "inspect"
	opSave    = "save the archive"
	opRead    = "read the archive"
	opUnpack  = "unpack"
	opPromote = "install the unpacked files"
	opDiscard = "discard the archive"
	opCleanup = "cleanup"
)

// Typed errors of the adapter (ARCH-32).
var (
	// ErrStorage is an I/O failure without a domain meaning; it wraps shared.ErrStorage.
	ErrStorage = fmt.Errorf("component %w", shared.ErrStorage)

	// ErrInvalidRoot means the data directory is unusable or components is not a plain directory.
	ErrInvalidRoot = errors.New("componentstore: invalid component folder")

	// ErrLinkedFolder means the installed folder is a link or not a directory; it does not count as the component.
	ErrLinkedFolder = errors.New("componentstore: the installed component folder is a link or not a directory")
)

// Store is the component folder below a data directory and the read-only folder of the executable. It is safe for
// concurrent use; Unpack, SaveArchive, DiscardArchive and Cleanup must not run at the same time (the use case runs
// one install at a time, after Cleanup).
type Store struct {
	root    *os.Root // <data dir>/components
	bundled string   // the folder of the executable; "" when unknown
}

// New opens <dataDir>/components, creating it if needed. bundledDir is the folder of the executable ("" when unknown).
func New(dataDir, bundledDir string) (*Store, error) {
	data, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, fail(opOpen, err)
	}
	defer func() { _ = data.Close() }()

	// Lstat first: Mkdir on a planted junction could create the folder elsewhere (as in filestore).
	if _, err := data.Lstat(componentsDir); errors.Is(err, fs.ErrNotExist) {
		if err := data.Mkdir(componentsDir, dirPerm); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fail(opCreate, err)
		}
	}
	fi, err := data.Lstat(componentsDir)
	if err != nil || !plainDir(fi) {
		return nil, fmt.Errorf("%w: %s is not a plain directory", ErrInvalidRoot, componentsDir)
	}
	root, err := data.OpenRoot(componentsDir)
	if err != nil {
		return nil, fail(opOpen, err)
	}
	return &Store{root: root, bundled: bundledDir}, nil
}

// InstalledDir is the folder the in-app install writes below dataDir, for the program path of the engine.
func InstalledDir(dataDir string) string { return filepath.Join(dataDir, componentsDir, installedDir) }

// Close releases the folder handle.
func (s *Store) Close() error { return s.root.Close() }

// Inspect reports what folder holds under the names of the component files: links, directories and other non-regular
// entries are reported as not regular; a regular file is hashed up to one byte over its pinned size. A missing folder
// holds nothing. A linked installed folder is ErrLinkedFolder.
func (s *Store) Inspect(ctx context.Context, folder values.ComponentFolder, spec values.SpeechComponent) ([]values.FoundFile, error) {
	r, err := s.openFolder(folder)
	if err != nil || r == nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	var out []values.FoundFile
	for _, f := range spec.Files() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found, ok, err := inspectFile(r, f)
		if err != nil {
			return nil, fail(opInspect+" "+f.Name(), err)
		}
		if ok {
			out = append(out, found)
		}
	}
	return out, nil
}

// openFolder opens the folder; nil without an error when it does not exist.
func (s *Store) openFolder(folder values.ComponentFolder) (*os.Root, error) {
	switch folder {
	case values.FolderBundled:
		if s.bundled == "" {
			return nil, nil
		}
		r, err := os.OpenRoot(s.bundled)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, fail(opOpen, err)
		}
		return r, nil
	case values.FolderInstalled:
		fi, err := s.root.Lstat(installedDir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, nil
		case err != nil:
			return nil, fail(opOpen, err)
		case !plainDir(fi):
			return nil, ErrLinkedFolder
		}
		r, err := s.root.OpenRoot(installedDir)
		if err != nil {
			return nil, fail(opOpen, err)
		}
		return r, nil
	}
	return nil, fmt.Errorf("componentstore: unknown folder %v", folder)
}

// inspectFile looks at one component file; ok is false when there is nothing under its name.
func inspectFile(r *os.Root, f values.ComponentFile) (values.FoundFile, bool, error) {
	fi, err := r.Lstat(f.Name())
	if errors.Is(err, fs.ErrNotExist) {
		return values.FoundFile{}, false, nil
	}
	if err != nil {
		return values.FoundFile{}, false, err
	}
	notRegular := values.FoundFile{Name: f.Name()}
	if !fi.Mode().IsRegular() {
		return notRegular, true, nil
	}

	file, err := r.Open(f.Name())
	if err != nil {
		return values.FoundFile{}, false, err
	}
	defer func() { _ = file.Close() }()
	// The entry may have been swapped between the Lstat and the open.
	if st, err := file.Stat(); err != nil || !st.Mode().IsRegular() || !os.SameFile(fi, st) {
		return notRegular, true, nil
	}
	n, sum, err := hashed(io.LimitReader(file, f.Size()+1))
	if err != nil {
		return values.FoundFile{}, false, err
	}
	return values.FoundFile{Name: f.Name(), Regular: true, Size: n, SHA256: sum}, true, nil
}

// SaveArchive writes the archive to a temporary file and renames it over the stored archive; it returns the size and
// SHA-256 of what it wrote. A failure leaves no temporary file behind.
func (s *Store) SaveArchive(ctx context.Context, content io.Reader) (int64, values.SHA256, error) {
	tmp := tmpPrefix + randomSuffix()
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return 0, "", fail(opSave, err)
	}
	h := sha256.New()
	src := &ctxReader{ctx: ctx, r: content}
	n, err := io.Copy(io.MultiWriter(f, h), src)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = s.replace(ctx, tmp, archiveName)
	}
	if err != nil {
		_ = s.root.Remove(tmp)
		if src.err != nil || ctx.Err() != nil {
			// The caller's reader failed (the network): its error, not a storage one.
			return n, "", errors.Join(src.err, ctx.Err())
		}
		return n, "", fail(opSave, err)
	}
	return n, values.SHA256(hex.EncodeToString(h.Sum(nil))), nil
}

// replace renames from over to, removing what is at to first: a link or a planted directory is removed, never
// followed (os.Root stays inside the folder).
func (s *Store) replace(ctx context.Context, from, to string) error {
	if err := retry(ctx, func() error { return s.root.RemoveAll(to) }); err != nil {
		return err
	}
	return retry(ctx, func() error { return s.root.Rename(from, to) })
}

// DiscardArchive removes the stored archive.
func (s *Store) DiscardArchive(ctx context.Context) error {
	if err := retry(ctx, func() error { return s.root.RemoveAll(archiveName) }); err != nil {
		return fail(opDiscard, err)
	}
	return nil
}

// Cleanup removes what an interrupted install left: temporary files, staging and retired folders and the archive. It
// returns how many entries it removed. Run it before any install.
func (s *Store) Cleanup(ctx context.Context) (int, error) {
	dir, err := s.root.Open(".")
	if err != nil {
		return 0, fail(opCleanup, err)
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return 0, fail(opCleanup, err)
	}

	removed := 0
	var errs []error
	for _, e := range entries {
		if !leftover(e.Name()) {
			continue
		}
		if err := retry(ctx, func() error { return s.root.RemoveAll(e.Name()) }); err != nil {
			errs = append(errs, fail(opCleanup, err))
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

func leftover(name string) bool {
	return name == archiveName || hasPrefix(name, tmpPrefix) || hasPrefix(name, stagingPrefix) || hasPrefix(name, retiredPrefix)
}

func hasPrefix(s, p string) bool { return len(s) > len(p) && s[:len(p)] == p }

// plainDir reports a directory that is not a link or another reparse point.
func plainDir(fi fs.FileInfo) bool {
	return fi.IsDir() && fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}

// hashed reads r to the end and returns how many bytes it had and their SHA-256.
func hashed(r io.Reader) (int64, values.SHA256, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return n, "", err
	}
	return n, values.SHA256(hex.EncodeToString(h.Sum(nil))), nil
}

// randomSuffix names a fresh temporary entry.
func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+)
	return hex.EncodeToString(b[:])
}

// retry repeats fn a few times: Windows reports a fresh file busy for a moment.
func retry(ctx context.Context, fn func() error) error {
	var err error
	for i := range retryAttempts {
		if err = fn(); err == nil {
			return nil
		}
		if i == retryAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(time.Duration(i+1) * retryStep):
		}
	}
	return err
}

// ctxReader stops on a cancelled context and remembers the error of the wrapped reader.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
	err error
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		c.err = err
	}
	return n, err
}

// storageError prints a path-free message and matches ErrStorage, a narrower shared error and the bare OS error.
type storageError struct {
	msg   string
	kinds []error
	cause error
}

func (e *storageError) Error() string   { return e.msg }
func (e *storageError) Unwrap() []error { return append(append([]error(nil), e.kinds...), e.cause) }

// fail wraps an OS error without its path; a context error is returned as it is.
func fail(op string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	bare := err
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		bare = pe.Err
	case errors.As(err, &le):
		bare = le.Err
	}
	kinds := []error{ErrStorage}
	if errors.Is(bare, fs.ErrPermission) {
		kinds = append(kinds, shared.ErrStoragePermission)
	}
	return &storageError{msg: fmt.Sprintf("%v: %s: %v", ErrStorage, op, bare), kinds: kinds, cause: bare}
}
