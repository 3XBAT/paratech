package soundcheck

import "errors"

var (
	// ErrInvalidConfig is returned by New when a port is missing or the duration is out of range.
	ErrInvalidConfig = errors.New("invalid sound check configuration")

	// ErrBusy means a check runs already; one runs at a time.
	ErrBusy = errors.New("soundcheck: a check is already running")

	// ErrClosed means the service was closed (the app shuts down).
	ErrClosed = errors.New("soundcheck: closed")
)
