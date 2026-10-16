// Package logfile keeps the log of the desktop app on disk (TASK-058): <DataDir>/logs/desktop.log, rotated by size
// into desktop.1.log and desktop.2.log, so the support bundle has the recent history after the console is gone.
// Tee feeds it from the app's logger; Dir reads the files back for the bundle. Standard library only.
//
// Errors never contain a path: the data directory lies below the user's profile.
package logfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Defaults of Options.
const (
	DefaultMaxBytes = 5 << 20 // per file
	DefaultKeep     = 2       // rotated files kept

	baseName = "desktop"
	ext      = ".log"

	dirPerm  = 0o700
	filePerm = 0o600
)

// Operation names used in errors.
const (
	opOpen  = "open the log file"
	opWrite = "write the log file"
	opRead  = "read the log files"
)

var (
	// ErrLogFile is any failure of the log file; the system error is wrapped without its path.
	ErrLogFile = errors.New("logfile: the log file failed")

	// ErrClosed means the file was closed.
	ErrClosed = errors.New("logfile: the log file is closed")
)

// Options tunes a File; zero fields take the defaults.
type Options struct {
	MaxBytes int64 // a write that would pass it rotates first
	Keep     int   // rotated files kept: desktop.1.log (newest) .. desktop.<Keep>.log
}

// File is the current log file of a directory, rotated by size. It is safe for concurrent use.
type File struct {
	dir  string
	max  int64
	keep int

	mu     sync.Mutex
	f      *os.File
	size   int64
	closed bool
}

// Open creates dir (owner only) and opens dir/desktop.log for appending.
func Open(dir string, opts Options) (*File, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: %s: the directory must be an absolute path", ErrLogFile, opOpen)
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Keep <= 0 {
		opts.Keep = DefaultKeep
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fail(opOpen, err)
	}
	l := &File{dir: dir, max: opts.MaxBytes, keep: opts.Keep}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

// Name is the file name of the current log, the newest of Names.
func Name() string { return baseName + ext }

// Names are the file names of the log, newest first, for keep rotated files.
func Names(keep int) []string {
	out := []string{Name()}
	for i := 1; i <= keep; i++ {
		out = append(out, baseName+"."+strconv.Itoa(i)+ext)
	}
	return out
}

func (l *File) open() error {
	f, err := os.OpenFile(filepath.Join(l.dir, Name()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return fail(opOpen, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fail(opOpen, err)
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Write appends p, after a rotation when p would pass the size limit. A rotation that fails (a reader holds a file
// open) keeps appending to the current file and is tried again by the next write.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		l.rotate()
	}
	if l.f == nil {
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	if err != nil {
		return n, fail(opWrite, err)
	}
	return n, nil
}

// rotate closes the current file and shifts the names: desktop.log becomes desktop.1.log and the oldest one is
// replaced. It reopens desktop.log in any case.
func (l *File) rotate() {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	names := Names(l.keep)
	ok := true
	for i := len(names) - 1; i > 0 && ok; i-- {
		err := os.Rename(filepath.Join(l.dir, names[i-1]), filepath.Join(l.dir, names[i]))
		ok = err == nil || errors.Is(err, fs.ErrNotExist)
	}
	_ = l.open() // a failure is reported by Write
}

// Close closes the current file; later writes fail with ErrClosed. It is safe to call more than once.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	if err != nil {
		return fail(opWrite, err)
	}
	return nil
}

// Dir reads the log files of a directory back, for the support bundle.
type Dir struct {
	Path string // the directory of the log files
	Keep int    // rotated files; 0 is DefaultKeep
}

// EachLog calls fn with the name and content of every log file there is, newest first, within maxBytes in all: of a
// file that does not fit, only its last part (the most recent lines) is read. Missing files are skipped.
func (d Dir) EachLog(ctx context.Context, maxBytes int64, fn func(name string, data []byte) error) error {
	keep := d.Keep
	if keep <= 0 {
		keep = DefaultKeep
	}
	left := maxBytes
	for _, name := range Names(keep) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if left <= 0 {
			return nil
		}
		data, err := readTail(filepath.Join(d.Path, name), left)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return err
		}
		left -= int64(len(data))
		if err := fn(name, data); err != nil {
			return err
		}
	}
	return nil
}

// readTail reads at most limit bytes from the end of a regular file, whole lines only: a cut first line could hold
// the cut end of a path that the bundle's %USERPROFILE% replacement no longer finds.
func readTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a fixed name in the app's log directory
	if err != nil {
		return nil, fail(opRead, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fail(opRead, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s: not a regular file", ErrLogFile, opRead)
	}
	off := info.Size() - limit
	if off <= 0 {
		data, err := io.ReadAll(io.LimitReader(f, limit))
		if err != nil {
			return nil, fail(opRead, err)
		}
		return data, nil
	}

	// One byte before the tail too: when it is a newline, the tail starts with a whole line.
	if _, err := f.Seek(off-1, io.SeekStart); err != nil {
		return nil, fail(opRead, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fail(opRead, err)
	}
	i := bytes.IndexByte(data, '\n')
	if i < 0 {
		return nil, nil // one line longer than the limit
	}
	return data[i+1:], nil
}

// pathFreeError is ErrLogFile with the operation and the bare system error, which stays reachable for errors.Is.
type pathFreeError struct {
	op    string
	cause error
}

func (e *pathFreeError) Error() string {
	return ErrLogFile.Error() + ": " + e.op + ": " + e.cause.Error()
}

func (e *pathFreeError) Unwrap() []error { return []error{ErrLogFile, e.cause} }

// fail drops the path of an OS error: a *fs.PathError or *os.LinkError prints it.
func fail(op string, err error) error {
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		err = pe.Err
	case errors.As(err, &le):
		err = le.Err
	}
	return &pathFreeError{op: op, cause: err}
}
