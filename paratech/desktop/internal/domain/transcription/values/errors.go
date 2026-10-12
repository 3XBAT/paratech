package values

import "errors"

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
)
