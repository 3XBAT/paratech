//go:build windows

package instancelock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"paratech/desktop/internal/domain/shared"
)

// lockFile takes an exclusive lock of the first byte. Windows locks belong to the handle, so a second handle of the
// same process is refused like another process.
func lockFile(f *os.File) error {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return ErrHeld
	}
	return fmt.Errorf("instancelock: lock: %w", shared.ErrStorage)
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
