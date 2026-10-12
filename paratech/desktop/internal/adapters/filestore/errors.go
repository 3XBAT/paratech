package filestore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"paratech/desktop/internal/domain/shared"
)

// Typed errors of the adapter (ARCH-32).
var (
	// ErrStorage is any I/O failure that has no domain meaning (disk full, access denied, file in use, ...).
	// It wraps shared.ErrStorage; the narrower shared.ErrStoragePermission, ErrStorageFull and ErrStorageBusy
	// are added where the OS error allows.
	ErrStorage = fmt.Errorf("artifact %w", shared.ErrStorage)

	// ErrInvalidRoot means the data root passed to New is unusable.
	ErrInvalidRoot = errors.New("invalid artifact root")
)

// storageError carries the cause for errors.Is but prints only the path-free message.
type storageError struct {
	msg   string
	kinds []error // ErrStorage and maybe a narrower shared error
	cause error   // bare OS error without a path
}

func (e *storageError) Error() string   { return e.msg }
func (e *storageError) Unwrap() []error { return append(append([]error(nil), e.kinds...), e.cause) }

// storageFail builds an ErrStorage error from an OS error without OS paths in the message.
func storageFail(what string, err error) error {
	bare := bareErr(err)
	kinds := []error{ErrStorage}
	switch k := osKind(bare); {
	case k != nil:
		kinds = append(kinds, k)
	case errors.Is(bare, fs.ErrPermission):
		kinds = append(kinds, shared.ErrStoragePermission)
	}
	return &storageError{msg: fmt.Sprintf("%v: %s: %s", ErrStorage, what, bare), kinds: kinds, cause: bare}
}

// mapErr translates an os error into a shared sentinel or an ErrStorage error.
func mapErr(op string, ref shared.ArtifactRef, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, shared.ErrInvalidArtifactRef), errors.Is(err, shared.ErrConflict):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s", shared.ErrNotFound, ref)
	}
	return storageFail(op+" "+ref.String(), err)
}

// bareErr drops the path that os errors carry.
func bareErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}
