// Package userfile reads a file the user chose in an open dialog, for the schedule import of the schedule page (PRD
// section 11.2). It implements the file reader port of input/wails (ARCH-30..32).
//
// Only regular files are read (no directories, devices or pipes), at most the limit plus one byte, so the caller sees
// that a file is over its limit without reading it whole. Errors are ErrNotRegular or ErrUnreadable for errors.Is and keep only
// the operation and the system error: the path never appears in them.
package userfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrNotRegular means the path is a directory, a device, a pipe or another non-regular file.
	ErrNotRegular = errors.New("userfile: not a regular file")
	// ErrUnreadable means the file cannot be opened or read; the system error is wrapped (fs.ErrNotExist, ...).
	ErrUnreadable = errors.New("userfile: the file cannot be read")
)

// Reader reads chosen files. The zero value is ready to use.
type Reader struct{}

// New returns a Reader.
func New() Reader { return Reader{} }

// ReadFile returns at most maxBytes+1 bytes of the regular file at path.
func (Reader) ReadFile(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: negative limit", ErrUnreadable)
	}
	if path == "" || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("%w: not a file path", ErrUnreadable)
	}
	clean := filepath.Clean(path)

	// Stat before the open: opening a pipe can block until a writer comes.
	before, err := os.Stat(clean)
	if err != nil {
		return nil, osError("stat", err)
	}
	if !before.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	// The path is what the user picked in the open dialog; the file is only read.
	f, err := os.Open(clean)
	if err != nil {
		return nil, osError("open", err)
	}
	defer func() { _ = f.Close() }()

	// Checked again on the open handle: the path may have been swapped for something else since the stat.
	after, err := f.Stat()
	if err != nil {
		return nil, osError("stat", err)
	}
	if !after.Mode().IsRegular() {
		return nil, ErrNotRegular
	}

	limit := maxBytes
	if limit < math.MaxInt64 {
		limit++
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, osError("read", err)
	}
	return data, nil
}

// pathFreeError is ErrUnreadable with the operation and the bare system error, which stays reachable for errors.Is.
type pathFreeError struct {
	op    string
	cause error
}

func (e *pathFreeError) Error() string {
	return ErrUnreadable.Error() + ": " + e.op + ": " + e.cause.Error()
}

func (e *pathFreeError) Unwrap() []error { return []error{ErrUnreadable, e.cause} }

// osError drops the path of an OS error: a *fs.PathError prints it.
func osError(op string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return &pathFreeError{op: op, cause: err}
}
