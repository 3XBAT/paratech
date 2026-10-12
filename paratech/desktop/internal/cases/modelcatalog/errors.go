package modelcatalog

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the model catalog use case (ARCH-18). Domain errors (sttmodel.ErrPinned, sttmodel.ErrFileInUse,
// modelselection.ErrNoDefaultModel, values.ErrChecksumMismatch, values.ErrReservedIdentity, ...) are returned
// wrapped, not translated.
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid model catalog configuration")

	// ErrInvalidRegistration means a Registration mixes the catalog and the import form.
	ErrInvalidRegistration = errors.New("invalid model registration")

	// ErrNotInCatalog means a Registration names a catalog id that the manifest does not have.
	ErrNotInCatalog = fmt.Errorf("%w: catalog has no such model", shared.ErrNotFound)

	// ErrAlreadyInstalled means a model with the same name and version is installed.
	ErrAlreadyInstalled = fmt.Errorf("%w: model with this name and version is installed", shared.ErrDuplicate)

	// ErrDefaultReselected means the model was selected as the default again each time its removal tried to clear it.
	ErrDefaultReselected = fmt.Errorf("%w: model was selected as default again during removal", shared.ErrConflict)
)

// errDefaultReselected signals one removal attempt that met the default selected again; Delete retries on it.
var errDefaultReselected = errors.New("default selected again")
