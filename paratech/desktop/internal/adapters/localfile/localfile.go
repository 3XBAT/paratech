// Package localfile is the adapter that reads a file the user picked outside the data directory, for an import: a
// model file (PRD sections 8 and 8.1) or an old recording (ADR-0027). It implements modelcatalog.ImportSource (Reader)
// and recordingimport.Originals (SeekReader) (ARCH-30..33).
//
// The path is untrusted input: it is cleaned and must be absolute and free of NUL bytes; on Windows it must be on a
// drive letter, so UNC, device and namespace paths, reserved device names and streams are refused. A mapped network
// drive and a symbolic link or junction the user picked are followed, even to a remote target. Only regular files
// are opened (no directories, devices or pipes). The file is opened read-only and checked again after the open, so a
// path swapped for another file in between is refused. Errors are values.ErrInvalidImportFile, shared.ErrNotFound or shared.ErrStorage
// (narrowed to shared.ErrStoragePermission when access is denied); the OS path never appears in messages.
package localfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// ErrStorage is a read failure of the picked file that has no domain meaning. It wraps shared.ErrStorage.
var ErrStorage = fmt.Errorf("import file %w", shared.ErrStorage)

// testHookAfterStat, when set by a test, runs between the first stat and the open, to swap the file.
var testHookAfterStat func(path string)

// Reader opens picked files. The zero value is ready to use.
type Reader struct{}

// New returns a Reader.
func New() *Reader { return &Reader{} }

// Open opens the regular file at the absolute path and returns it with its size; the caller closes it.
func (*Reader) Open(ctx context.Context, path string) (io.ReadCloser, int64, error) {
	f, size, err := open(ctx, path)
	if err != nil {
		return nil, 0, err // not a nil *file in a non-nil interface
	}
	return f, size, nil
}

// SeekReader opens picked files that are read out of order: the head and the tail of an audio file before its copy
// (recordingimport.Originals). The zero value is ready to use.
type SeekReader struct{}

// NewSeekReader returns a SeekReader.
func NewSeekReader() *SeekReader { return &SeekReader{} }

// Open opens the regular file at the absolute path as Reader.Open does, seekable.
func (*SeekReader) Open(ctx context.Context, path string) (io.ReadSeekCloser, int64, error) {
	f, size, err := open(ctx, path)
	if err != nil {
		return nil, 0, err
	}
	return f, size, nil
}

func open(ctx context.Context, path string) (*file, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if path == "" || strings.ContainsRune(path, 0) {
		return nil, 0, fmt.Errorf("%w: not a file path", values.ErrInvalidImportFile)
	}
	clean := filepath.Clean(path)
	if err := checkPath(clean); err != nil {
		return nil, 0, err
	}

	// Stat before the open: opening a pipe can block.
	before, err := os.Stat(clean)
	if err != nil {
		return nil, 0, mapErr("stat", err)
	}
	if !before.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("%w: %s", values.ErrInvalidImportFile, kind(before))
	}
	// On Windows os.Stat does not read the file id; os.SameFile reads it lazily by reopening the path. Read it now, or
	// the check after the open would compare the open file with whatever is at the path then. It fails only when the
	// id cannot be read.
	if !os.SameFile(before, before) {
		return nil, 0, fmt.Errorf("%w: the file cannot be identified", values.ErrInvalidImportFile)
	}
	if testHookAfterStat != nil {
		testHookAfterStat(clean)
	}

	// Any local file the user picked is legitimate; checkPath refused everything else.
	f, err := os.Open(clean)
	if err != nil {
		return nil, 0, mapErr("open", err)
	}
	after, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, mapErr("stat", err)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = f.Close()
		return nil, 0, fmt.Errorf("%w: the file was replaced while it was opened", values.ErrInvalidImportFile)
	}
	return &file{f: f}, after.Size(), nil
}

// checkPath refuses a cleaned path that is relative or fails the platform check with values.ErrInvalidImportFile.
func checkPath(clean string) error {
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("%w: not an absolute path", values.ErrInvalidImportFile)
	}
	return checkLocal(clean)
}

// file maps read errors so that the OS path does not leak.
type file struct{ f *os.File }

func (r *file) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, mapErr("read", err)
	}
	return n, err
}

func (r *file) Seek(offset int64, whence int) (int64, error) {
	n, err := r.f.Seek(offset, whence)
	return n, mapErr("seek", err)
}

func (r *file) Close() error { return mapErr("close", r.f.Close()) }

func kind(fi fs.FileInfo) string {
	switch m := fi.Mode(); {
	case m.IsDir():
		return "a directory"
	case m&(fs.ModeDevice|fs.ModeCharDevice) != 0:
		return "a device"
	case m&fs.ModeNamedPipe != 0:
		return "a pipe"
	}
	return "not a regular file"
}

// pathFreeError carries the bare OS error for errors.Is but prints no path.
type pathFreeError struct {
	msg   string
	kinds []error
	cause error
}

func (e *pathFreeError) Error() string   { return e.msg }
func (e *pathFreeError) Unwrap() []error { return append(append([]error(nil), e.kinds...), e.cause) }

// mapErr translates an OS error into shared.ErrNotFound or an ErrStorage error without the path.
func mapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	bare := err
	var pe *fs.PathError
	if errors.As(err, &pe) {
		bare = pe.Err
	}

	switch {
	case errors.Is(bare, fs.ErrNotExist):
		return &pathFreeError{msg: "import file: " + op + ": " + shared.ErrNotFound.Error(), kinds: []error{shared.ErrNotFound}, cause: bare}
	case errors.Is(bare, fs.ErrPermission):
		return &pathFreeError{msg: fmt.Sprintf("%v: %s: %v", ErrStorage, op, bare), kinds: []error{ErrStorage, shared.ErrStoragePermission}, cause: bare}
	}
	return &pathFreeError{msg: fmt.Sprintf("%v: %s: %v", ErrStorage, op, bare), kinds: []error{ErrStorage}, cause: bare}
}
