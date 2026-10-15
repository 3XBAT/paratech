package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"paratech/desktop/internal/domain/shared"
)

// Operation names of the partial-file operations.
const (
	opAppend    = "append"
	opCreate    = "create"
	opRename    = "rename"
	opFreeSpace = "free space"
)

// Create makes a new empty file for writing, creating its directory, and returns it open; the caller writes, syncs
// and closes it. It is exclusive: an existing entry is shared.ErrConflict, so a file is never overwritten. It is not
// atomic, on purpose: the audio recorder grows a chunk under a temporary name and renames it when complete (Rename).
// Links are refused as everywhere.
func (s *Store) Create(ctx context.Context, ref shared.ArtifactRef) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parts, err := locate(ref)
	if err != nil {
		return nil, err
	}

	fi, err := s.inspect(parts, false)
	switch {
	case errors.Is(err, errNotDir):
		return nil, fmt.Errorf("%w: a parent of %s is a file", shared.ErrConflict, ref)
	case err != nil:
		return nil, mapErr(opCreate, ref, err)
	case fi != nil:
		return nil, fmt.Errorf("%w: %s exists", shared.ErrConflict, ref)
	}

	rel := filepath.Join(parts...)
	if err := s.root.MkdirAll(filepath.Dir(rel), dirPerm); err != nil {
		return nil, writeErr(ref, err)
	}

	// O_EXCL also covers a file (or a link) that appeared after the inspection.
	f, err := s.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	switch {
	case errors.Is(err, fs.ErrExist):
		return nil, fmt.Errorf("%w: %s exists", shared.ErrConflict, ref)
	case err != nil:
		return nil, writeErr(ref, err)
	}
	return f, nil
}

// Append adds the content to the end of a file, creating it (and its directory) if needed, and flushes it to disk.
// It is not atomic, on purpose: it grows a partial download, and the bytes written before a failure stay, so the
// download can resume. It returns how many bytes it wrote, also on error. A failure of the content reader is
// returned wrapped as it is; a failure of the disk is an ErrStorage error.
func (s *Store) Append(ctx context.Context, ref shared.ArtifactRef, content io.Reader) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	parts, err := locate(ref)
	if err != nil {
		return 0, err
	}

	fi, err := s.inspect(parts, false)
	switch {
	case errors.Is(err, errNotDir):
		return 0, fmt.Errorf("%w: a parent of %s is a file", shared.ErrConflict, ref)
	case err != nil:
		return 0, mapErr(opAppend, ref, err)
	case fi != nil && fi.IsDir():
		return 0, isDirErr(ref)
	}

	rel := filepath.Join(parts...)
	if err := s.root.MkdirAll(filepath.Dir(rel), dirPerm); err != nil {
		return 0, mapErr(opAppend, ref, err)
	}

	f, err := s.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return 0, mapErr(opAppend, ref, err)
	}

	src := &errTrackReader{ctx: ctx, r: content}
	n, err := io.Copy(f, src)

	// Flush what arrived even after a failure: it is the resume point.
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return n, err
	case src.err != nil && errors.Is(err, src.err):
		return n, fmt.Errorf("read content of %s: %w", ref, err)
	}
	return n, mapErr(opAppend, ref, err)
}

// Rename moves a file to a new reference that does not exist yet, creating its directory, for example a verified
// download to its installed name. Links are refused on both sides; an existing target is shared.ErrConflict and a
// missing source shared.ErrNotFound.
func (s *Store) Rename(ctx context.Context, from, to shared.ArtifactRef) error {
	src, fi, err := s.lookup(ctx, opRename, from, false)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return isDirErr(from)
	}

	parts, err := locate(to)
	if err != nil {
		return err
	}
	dfi, err := s.inspect(parts, false)
	switch {
	case errors.Is(err, errNotDir):
		return fmt.Errorf("%w: a parent of %s is a file", shared.ErrConflict, to)
	case err != nil:
		return mapErr(opRename, to, err)
	case dfi != nil:
		return fmt.Errorf("%w: %s exists", shared.ErrConflict, to)
	}

	dst := filepath.Join(parts...)
	dir := filepath.Dir(dst)
	if err := s.root.MkdirAll(dir, dirPerm); err != nil {
		return mapErr(opRename, to, err)
	}

	if err := retry(ctx, func() error { return s.root.Rename(src, dst) }); err != nil {
		return mapErr(opRename, from, err)
	}
	syncDir(s.root, dir)
	return nil
}

// FreeBytes returns the free disk space available to this user on the volume of the data root.
func (s *Store) FreeBytes(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	n, err := freeBytes(s.root.Name())
	if err != nil {
		if errors.Is(err, errors.ErrUnsupported) {
			return 0, err
		}
		return 0, storageFail(opFreeSpace, err)
	}
	return n, nil
}
