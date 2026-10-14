package activity

import "errors"

// ErrInvalidConfig is returned by New when a port is missing.
var ErrInvalidConfig = errors.New("invalid activity configuration")
