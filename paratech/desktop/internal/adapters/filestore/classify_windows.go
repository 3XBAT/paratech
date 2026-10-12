package filestore

import (
	"errors"
	"syscall"

	"paratech/desktop/internal/domain/shared"
)

// Win32 codes that have no portable errors.Is target.
const (
	errSharingViolation syscall.Errno = 32
	errLockViolation    syscall.Errno = 33
	errHandleDiskFull   syscall.Errno = 39
	errDiskFull         syscall.Errno = 112
)

// osKind classifies a bare OS error as a narrower shared storage error, or nil.
func osKind(err error) error {
	switch {
	case errors.Is(err, errDiskFull), errors.Is(err, errHandleDiskFull):
		return shared.ErrStorageFull
	case errors.Is(err, errSharingViolation), errors.Is(err, errLockViolation):
		return shared.ErrStorageBusy
	}
	return nil
}
