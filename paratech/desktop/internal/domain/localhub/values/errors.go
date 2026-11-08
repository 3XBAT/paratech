package values

import "errors"

// Errors of the local Hub value objects (ARCH-18). Messages never quote the rejected input: a URL may carry a
// password and a misplaced key must not reach a log.
var (
	// ErrNotLocal means «Адрес сервера» is not a loopback Target: the desktop uses a remote Hub.
	ErrNotLocal = errors.New("server address is not a local hub")

	// ErrInvalidSettings is returned by NewServerSettings for a base URL, auth mode or quota limit the Hub refuses.
	ErrInvalidSettings = errors.New("invalid local hub settings")

	// ErrUnknownSecret is returned for a secret slot name outside the closed list, or one that cannot be typed in.
	ErrUnknownSecret = errors.New("unknown local hub secret")
)
