package speechcomponent

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the speech component use case (ARCH-18). Domain errors (values.ErrChecksumMismatch,
// values.ErrSizeMismatch, values.ErrHostNotAllowed, values.ErrInsufficientSpace, values.ErrInvalidComponentArchive, ...)
// are returned wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid speech component configuration")

	// ErrInstallInProgress means an install runs already.
	ErrInstallInProgress = fmt.Errorf("%w: the speech component is being installed", shared.ErrConflict)

	// ErrAlreadyActive means speech recognition already works; there is nothing to install.
	ErrAlreadyActive = fmt.Errorf("%w: speech recognition is active", shared.ErrConflict)

	// ErrClosed means the service is shutting down.
	ErrClosed = errors.New("speech component service closed")

	// ErrDownloadFailed means the archive could not be fetched: no connection, a server error, a broken transfer.
	ErrDownloadFailed = errors.New("speech component download failed")

	// ErrNotVerified means the installed files did not verify after the install.
	ErrNotVerified = errors.New("installed speech component files do not verify")

	// ErrActivationFailed means the files verify but speech recognition could not be started with them.
	ErrActivationFailed = errors.New("speech recognition could not be activated")
)
