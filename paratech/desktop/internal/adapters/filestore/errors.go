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
	ErrStorage = errors.New("artifact storage failure")

	// ErrInvalidRoot means the data root passed to New is unusable.
	ErrInvalidRoot = errors.New("invalid artifact root")
)

// mapErr translates an os error into a shared sentinel or ErrStorage without OS paths in the message.
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
	return fmt.Errorf("%w: %s %s: %s", ErrStorage, op, ref, reason(err))
}

// reason returns the cause text without the file path that os errors carry.
func reason(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error()
	}
	return err.Error()
}
