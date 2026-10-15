// Package instancelock makes one process the only user of a data directory: the single-instance guard of the desktop
// core. It holds an exclusive OS lock on <dir>/learning-assistant.lock for as long as the process keeps it; the OS
// drops the lock when the process dies, so a crash never leaves a stale lock behind. The file is never deleted:
// deleting it would let a second process lock a new file while the first still holds the old one.
package instancelock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"paratech/desktop/internal/domain/shared"
)

// FileName is the lock file below the data directory.
const FileName = "learning-assistant.lock"

const filePerm = 0o600

var (
	// ErrHeld means another process (or another Lock of this one) holds the data directory.
	ErrHeld = errors.New("instancelock: the data directory is used by another instance")

	// ErrUnsupported means this operating system has no implementation.
	ErrUnsupported = errors.New("instancelock: not supported on this system")
)

// Lock is a held lock. Release it with Release.
type Lock struct {
	mu sync.Mutex
	f  *os.File
}

// Acquire takes the lock of the absolute directory dir, which must exist. It fails with ErrHeld at once, without
// waiting, if the lock is taken.
func Acquire(dir string) (*Lock, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("instancelock: the directory must be an absolute path")
	}

	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_RDWR|os.O_CREATE, filePerm) //nolint:gosec // the app's own data directory and a fixed name
	if err != nil {
		return nil, fmt.Errorf("instancelock: open the lock file: %w: %w", shared.ErrStorage, bare(err))
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release drops the lock. It is safe to call more than once.
func (l *Lock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.f == nil {
		return nil
	}
	// Closing the handle drops the lock too; the explicit unlock only makes it immediate.
	uerr := unlockFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err := errors.Join(uerr, cerr); err != nil {
		return fmt.Errorf("instancelock: release: %w: %w", shared.ErrStorage, bare(err))
	}
	return nil
}

// bare drops the path from an OS error: the data directory lies below the user's profile.
func bare(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
