package localhub

import "errors"

// Errors of the use case. None of them carries a secret value.
var (
	// ErrInvalidConfig means New was given a missing port.
	ErrInvalidConfig = errors.New("localhub: invalid configuration")

	// ErrEmptySecret means SetSecret was given an empty or blank value.
	ErrEmptySecret = errors.New("localhub: empty secret")

	// ErrSecretStore means the secret store failed; its own error is dropped because it quoted the value.
	ErrSecretStore = errors.New("localhub: secret store failed")
)
