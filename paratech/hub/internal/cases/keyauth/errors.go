package keyauth

import "errors"

var (
	// ErrInvalidKey means the presented key is malformed, unknown, revoked or has a wrong secret. The reason is
	// joined to it (apikey.ErrMalformed, ErrUnknown, ErrRevoked, ErrSecretMismatch) for server-side logs only; the
	// client must see one and the same answer for all of them.
	ErrInvalidKey = errors.New("keyauth: invalid API key")

	// ErrInvalidConfig means New got a nil port.
	ErrInvalidConfig = errors.New("keyauth: invalid configuration")
)
