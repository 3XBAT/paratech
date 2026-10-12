//go:build !windows

package filestore

import (
	"errors"
	"syscall"

	"paratech/desktop/internal/domain/shared"
)

// osKind classifies a bare OS error as a narrower shared storage error, or nil.
func osKind(err error) error {
	switch {
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return shared.ErrStorageFull
	case errors.Is(err, syscall.EBUSY), errors.Is(err, syscall.ETXTBSY):
		return shared.ErrStorageBusy
	}
	return nil
}
