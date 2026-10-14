package preferences

import "errors"

// ErrInvalidConfig is returned by New when a port is missing.
var ErrInvalidConfig = errors.New("invalid preferences configuration")
