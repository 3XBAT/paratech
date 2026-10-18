// Package textfile writes a text file to a path the user chose in a save dialog: the transcript export (TASK-079).
// The file is written to a temporary file next to the target and renamed over it only when complete, so a failure
// never leaves half a file under the chosen name. Standard library only.
//
// Errors never contain the path (it lies below the user's profile) or the content (the transcript).
package textfile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Operation names used in errors.
const (
	opCheck  = "check the target"
	opCreate = "create the file"
	opWrite  = "write the file"
	opFinish = "finish the file"
	opRename = "rename the file"
)

// MaxBytes bounds one file; a transcript of a day of speech is a few MiB.
const MaxBytes = 256 << 20

var (
	// ErrNotRegular means the target exists and is a directory, a link or another non-regular file.
	ErrNotRegular = errors.New("textfile: the target is not a regular file")

	// ErrTooLarge means the content exceeds MaxBytes.
	ErrTooLarge = errors.New("textfile: the content is too large")

	// ErrWrite means the file cannot be written; the system error is wrapped without its path.
	ErrWrite = errors.New("textfile: the file cannot be written")
)

// Writer writes text files. The zero value is ready to use.
type Writer struct{}

// New returns a Writer.
func New() Writer { return Writer{} }

// WriteFile replaces the file at path with content, atomically. On any failure the temporary file is removed and
// the target is untouched.
func (Writer) WriteFile(ctx context.Context, path string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(content) > MaxBytes {
		return ErrTooLarge
	}
	if path == "" || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s: not an absolute file path", ErrWrite, opCheck)
	}
	path = filepath.Clean(path)
	if err := checkTarget(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".paratech-export-*.tmp")
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

	if _, err := tmp.Write(content); err != nil {
		return fail(opWrite, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fail(opFinish, err)
	}
	if err := tmp.Close(); err != nil {
		return fail(opFinish, err)
	}
	// Checked again: the target may have been swapped for a directory or a link meanwhile.
	if err := checkTarget(path); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fail(opRename, err)
	}
	done = true
	return nil
}

// checkTarget refuses a target that exists and is not a regular file.
func checkTarget(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fail(opCheck, err)
	case !info.Mode().IsRegular():
		return ErrNotRegular
	}
	return nil
}

// pathFreeError is ErrWrite with the operation and the bare system error, which stays reachable for errors.Is.
type pathFreeError struct {
	op    string
	cause error
}

func (e *pathFreeError) Error() string {
	return ErrWrite.Error() + ": " + e.op + ": " + e.cause.Error()
}

func (e *pathFreeError) Unwrap() []error { return []error{ErrWrite, e.cause} }

// fail drops the path of an OS error: a *fs.PathError or *os.LinkError prints it.
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
	return &pathFreeError{op: op, cause: err}
}
