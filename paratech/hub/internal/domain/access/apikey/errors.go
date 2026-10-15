package apikey

import "errors"

// Domain errors of the aggregate (ARCH-18). None of them carries any part of a presented key.
var (
	// ErrMalformed means a presented key does not have the issued format.
	ErrMalformed = errors.New("malformed API key")

	// ErrUnknown means no key with the presented id exists.
	ErrUnknown = errors.New("unknown API key")

	// ErrSecretMismatch means the presented secret does not match the stored hash.
	ErrSecretMismatch = errors.New("API key secret does not match")

	// ErrRevoked means the key was revoked.
	ErrRevoked = errors.New("API key is revoked")

	// ErrAlreadyRevoked means Revoke was called on a revoked key.
	ErrAlreadyRevoked = errors.New("API key is already revoked")

	// ErrInvalidLabel means a label does not satisfy the label rules (see NewLabel).
	ErrInvalidLabel = errors.New("invalid API key label")

	// ErrInvalidMaterial means the random bytes for a new key have the wrong length or are all zero.
	ErrInvalidMaterial = errors.New("invalid API key material")

	// ErrInvalid means New or Restore got data that breaks an invariant of the aggregate.
	ErrInvalid = errors.New("invalid API key record")
)
