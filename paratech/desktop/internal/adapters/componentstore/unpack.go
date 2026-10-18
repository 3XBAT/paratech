package componentstore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"paratech/desktop/internal/domain/transcription/values"
)

// zipEncrypted is the general purpose flag of an encrypted entry.
const zipEncrypted = 0x1

// Unpack takes the component files out of the stored archive into a fresh staging folder and swaps it in as the
// installed folder only when every file verified. A failure removes the staging folder and leaves the installed
// folder as it was; only a failed swap itself can leave it missing (the next Detect then says so).
func (s *Store) Unpack(ctx context.Context, spec values.SpeechComponent) error {
	data, err := s.readArchive(spec)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%w: %w", values.ErrInvalidComponentArchive, err)
	}
	entries, err := selectEntries(zr, spec)
	if err != nil {
		return err
	}

	staging := stagingPrefix + randomSuffix()
	if err := s.root.Mkdir(staging, dirPerm); err != nil {
		return fail(opUnpack, err)
	}
	if err := s.fill(ctx, staging, spec, entries); err != nil {
		_ = retry(context.WithoutCancel(ctx), func() error { return s.root.RemoveAll(staging) })
		return err
	}
	if err := s.promote(ctx, staging); err != nil {
		_ = retry(context.WithoutCancel(ctx), func() error { return s.root.RemoveAll(staging) })
		return fail(opPromote, err)
	}
	return nil
}

// readArchive returns the stored archive when it is a regular file with the pinned size and SHA-256. It is read into
// memory (the size is bounded by the manifest), so the bytes that are unpacked are the bytes that were checked.
func (s *Store) readArchive(spec values.SpeechComponent) ([]byte, error) {
	fi, err := s.root.Lstat(archiveName)
	if err != nil {
		return nil, fail(opRead, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: the stored archive is not a regular file", values.ErrInvalidComponentArchive)
	}
	if fi.Size() != spec.ArchiveSize() {
		return nil, spec.VerifyArchive("", fi.Size())
	}

	f, err := s.root.Open(archiveName)
	if err != nil {
		return nil, fail(opRead, err)
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || !os.SameFile(fi, st) {
		return nil, fmt.Errorf("%w: the stored archive changed while it was opened", values.ErrInvalidComponentArchive)
	}
	data, err := io.ReadAll(io.LimitReader(f, spec.ArchiveSize()+1))
	if err != nil {
		return nil, fail(opRead, err)
	}
	sum := sha256.Sum256(data)
	if err := spec.VerifyArchive(values.SHA256(hex.EncodeToString(sum[:])), int64(len(data))); err != nil {
		return nil, err
	}
	return data, nil
}

// selectEntries maps every component file to its archive entry. Only entries named exactly <prefix><file> count; such
// an entry must appear once, be a plain unencrypted file stored or deflated, and declare the pinned size. Every other
// entry is ignored: its name never becomes a path.
func selectEntries(zr *zip.Reader, spec values.SpeechComponent) (map[string]*zip.File, error) {
	out := map[string]*zip.File{}
	for _, zf := range zr.File {
		cf, ok := spec.EntryFile(zf.Name)
		if !ok {
			continue
		}
		switch {
		case out[cf.Name()] != nil:
			return nil, fmt.Errorf("%w: %s appears twice", values.ErrInvalidComponentArchive, cf.Name())
		case !zf.Mode().IsRegular():
			return nil, fmt.Errorf("%w: %s is not a plain file", values.ErrInvalidComponentArchive, cf.Name())
		case zf.Flags&zipEncrypted != 0:
			return nil, fmt.Errorf("%w: %s is encrypted", values.ErrInvalidComponentArchive, cf.Name())
		case zf.Method != zip.Store && zf.Method != zip.Deflate:
			return nil, fmt.Errorf("%w: %s uses compression method %d", values.ErrInvalidComponentArchive, cf.Name(), zf.Method)
		case zf.UncompressedSize64 != uint64(cf.Size()):
			return nil, fmt.Errorf("%w: %w: %s declares %d bytes, want %d", values.ErrInvalidComponentArchive,
				values.ErrSizeMismatch, cf.Name(), zf.UncompressedSize64, cf.Size())
		}
		out[cf.Name()] = zf
	}
	for _, f := range spec.Files() {
		if out[f.Name()] == nil {
			return nil, fmt.Errorf("%w: %s%s is missing", values.ErrInvalidComponentArchive, spec.EntryPrefix(), f.Name())
		}
	}
	return out, nil
}

// fill writes every component file into the staging folder, each under its manifest name.
func (s *Store) fill(ctx context.Context, staging string, spec values.SpeechComponent, entries map[string]*zip.File) error {
	dir, err := s.root.OpenRoot(staging)
	if err != nil {
		return fail(opUnpack, err)
	}
	defer func() { _ = dir.Close() }()

	for _, f := range spec.Files() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeEntry(ctx, dir, entries[f.Name()], f); err != nil {
			return err
		}
	}
	return nil
}

// writeEntry copies one entry, at most one byte over its pinned size, hashing it on the way; a size or digest that
// differs from the manifest fails it.
func writeEntry(ctx context.Context, dir *os.Root, zf *zip.File, f values.ComponentFile) error {
	rc, err := zf.Open()
	if err != nil {
		return fmt.Errorf("%w: %s: %w", values.ErrInvalidComponentArchive, f.Name(), err)
	}
	defer func() { _ = rc.Close() }()

	out, err := dir.OpenFile(f.Name(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return fail(opUnpack+" "+f.Name(), err)
	}
	h := sha256.New()
	src := &ctxReader{ctx: ctx, r: io.LimitReader(rc, f.Size()+1)}
	n, err := io.Copy(io.MultiWriter(out, h), src)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case src.err != nil:
		// archive/zip reports a bad checksum or a malformed stream here.
		return fmt.Errorf("%w: %s: %w", values.ErrInvalidComponentArchive, f.Name(), src.err)
	case err != nil:
		return fail(opUnpack+" "+f.Name(), err)
	}
	return f.Verify(values.SHA256(hex.EncodeToString(h.Sum(nil))), n)
}

// promote swaps the staging folder in as the installed folder. An old installed folder is first renamed aside and
// removed afterwards (the next Cleanup removes it if that fails); a link in its place is removed, never followed.
func (s *Store) promote(ctx context.Context, staging string) error {
	retired := ""
	fi, err := s.root.Lstat(installedDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case plainDir(fi):
		retired = retiredPrefix + randomSuffix()
		if err := retry(ctx, func() error { return s.root.Rename(installedDir, retired) }); err != nil {
			return err
		}
	default:
		if err := retry(ctx, func() error { return s.root.Remove(installedDir) }); err != nil {
			return err
		}
	}

	if err := retry(ctx, func() error { return s.root.Rename(staging, installedDir) }); err != nil {
		if retired != "" {
			_ = s.root.Rename(retired, installedDir)
		}
		return err
	}
	if retired != "" {
		_ = retry(context.WithoutCancel(ctx), func() error { return s.root.RemoveAll(retired) })
	}
	return nil
}
