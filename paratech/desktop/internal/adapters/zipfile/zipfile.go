// Package zipfile writes a zip archive to a path the user chose in a save dialog: the support bundle (TASK-058,
// AC-4). The archive is built in a temporary file next to the target and renamed over it only when complete, so a
// failure never leaves half an archive under the chosen name. Standard library only.
//
// Errors never contain the path: it lies below the user's profile.
package zipfile

import (
	"archive/zip"
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
	opCreate = "create the archive"
	opAdd    = "add an entry"
	opFinish = "finish the archive"
	opRename = "rename the archive"
)

var (
	// ErrNotRegular means the target exists and is a directory, a link or another non-regular file.
	ErrNotRegular = errors.New("zipfile: the target is not a regular file")

	// ErrEntryName means an entry name is empty, absolute, has a backslash, a colon or a "." or ".." element.
	ErrEntryName = errors.New("zipfile: invalid entry name")

	// ErrWrite means the archive cannot be written; the system error is wrapped without its path.
	ErrWrite = errors.New("zipfile: the archive cannot be written")
)

// Writer writes archives. The zero value is ready to use.
type Writer struct{}

// New returns a Writer.
func New() Writer { return Writer{} }

// WriteZip creates the archive at path: fill adds the entries through add, in order. When fill or any step fails, the
// temporary file is removed and the target is untouched.
func (Writer) WriteZip(ctx context.Context, path string, fill func(add func(name string, data []byte) error) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %s: not an absolute file path", ErrWrite, opCheck)
	}
	path = filepath.Clean(path)
	if err := checkTarget(path); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".paratech-support-*.tmp")
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

	zw := zip.NewWriter(tmp)
	add := func(name string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := CheckName(name); err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return fail(opAdd, err)
		}
		if _, err := w.Write(data); err != nil {
			return fail(opAdd, err)
		}
		return nil
	}
	if err := fill(add); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fail(opFinish, err)
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

// CheckName accepts a relative slash-separated entry name without "." or ".." elements, backslashes, colons (drive
// letters) or control characters, so no unzip tool can place the entry outside its folder.
func CheckName(name string) error {
	bad := name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, `\:`) ||
		strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f })
	for part := range strings.SplitSeq(name, "/") {
		bad = bad || part == "" || part == "." || part == ".."
	}
	if bad {
		return fmt.Errorf("%w: %q", ErrEntryName, name)
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
