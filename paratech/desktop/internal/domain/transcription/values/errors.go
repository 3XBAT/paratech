package values

import (
	"errors"
	"fmt"
)

// Errors of the transcription value objects (ARCH-18).
var (
	// ErrInvalidModelName is returned for a model name outside the allowed charset or length.
	ErrInvalidModelName = errors.New("invalid model name")

	// ErrInvalidModelVersion is returned for a model version outside the allowed charset or length.
	ErrInvalidModelVersion = errors.New("invalid model version")

	// ErrInvalidCatalogID is returned for a catalog entry id outside the allowed charset or length.
	ErrInvalidCatalogID = errors.New("invalid catalog id")

	// ErrInvalidSHA256 is returned for a checksum that is not 64 hex digits.
	ErrInvalidSHA256 = errors.New("invalid sha-256")

	// ErrInvalidSourceURL is returned for a source URL that is not a plain https URL without credentials.
	ErrInvalidSourceURL = errors.New("invalid source url")

	// ErrInvalidQuantization is returned for a quantization label outside the allowed charset or length.
	ErrInvalidQuantization = errors.New("invalid quantization")

	// ErrInvalidCapability is returned for a capability label outside the allowed charset or length.
	ErrInvalidCapability = errors.New("invalid capability")

	// ErrInvalidModelTier is returned for a model tier that is not one of the known tiers.
	ErrInvalidModelTier = errors.New("invalid model tier")

	// ErrInvalidModelFamily is returned for a model family that is not one of the known families.
	ErrInvalidModelFamily = errors.New("invalid model family")

	// ErrInvalidSize is returned for a model size that is not positive or above MaxModelSizeBytes.
	ErrInvalidSize = errors.New("invalid model size")

	// ErrInvalidMachine is returned by NewMachine for a negative or half-known hardware profile.
	ErrInvalidMachine = errors.New("invalid machine profile")

	// ErrInvalidCatalog is returned by ParseCatalog for a manifest that is malformed or breaks a rule.
	ErrInvalidCatalog = errors.New("invalid model catalog")

	// ErrChecksumMismatch means a file does not have the SHA-256 the catalog promises (PRD section 14 case 13).
	ErrChecksumMismatch = errors.New("checksum mismatch")

	// ErrSizeMismatch means a file does not have the size the catalog promises.
	ErrSizeMismatch = errors.New("size mismatch")

	// ErrReservedIdentity means an imported model claims the name and version of a catalog entry.
	ErrReservedIdentity = errors.New("name and version belong to a catalog model")

	// ErrInvalidHost is returned for a download host that is not a plain ASCII host name or IP literal.
	ErrInvalidHost = errors.New("invalid download host")

	// ErrHostNotAllowed means a download or one of its redirects goes to a host outside the catalog allowlist
	// (PRD sections 8.1 and 13.2).
	ErrHostNotAllowed = errors.New("download host is not allowed")

	// ErrUnexpectedRange means a server replied to a download request with bytes that do not continue the file.
	ErrUnexpectedRange = errors.New("unexpected download range")

	// ErrInsufficientSpace means the disk has too little free space for a download (PRD section 8); the error is an
	// *InsufficientSpaceError with the numbers.
	ErrInsufficientSpace = errors.New("not enough free disk space")

	// ErrInvalidSTTRequest is returned for an STT job payload that is malformed or names no valid recording.
	ErrInvalidSTTRequest = errors.New("invalid stt job payload")

	// ErrInvalidImportFile means the file chosen for an import is not a regular file at an absolute local path.
	ErrInvalidImportFile = errors.New("not a regular file that can be imported")

	// ErrConfirmationRequired means a model outside the catalog is imported without explicit user confirmation
	// (AC-23).
	ErrConfirmationRequired = errors.New("import of a model outside the catalog needs user confirmation")
)

// Narrower errors; errors.Is also matches the error they wrap.
var (
	// ErrImportTooLarge means a file imported outside the catalog is larger than MaxImportSizeBytes.
	ErrImportTooLarge = fmt.Errorf("%w: import file too large", ErrInvalidSize)

	// ErrConfirmationMismatch means the file to install is not the one whose SHA-256 the user confirmed
	// (PRD section 14 case 13).
	ErrConfirmationMismatch = fmt.Errorf("%w: file is not the one the user confirmed", ErrChecksumMismatch)
)
