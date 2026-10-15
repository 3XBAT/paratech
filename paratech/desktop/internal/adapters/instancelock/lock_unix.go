//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package instancelock

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"paratech/desktop/internal/domain/shared"
)

// lockFile takes an exclusive flock. It belongs to the open file, so a second open of the same process is refused
// like another process.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return ErrHeld
	}
	// The errno carries no path; it tells why the lock failed.
	return fmt.Errorf("instancelock: lock: %w: %w", shared.ErrStorage, err)
}

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
