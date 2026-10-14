package scheduler

import "errors"

// ErrInvalidConfig is returned by New when a port is missing or the configuration is out of range.
var ErrInvalidConfig = errors.New("invalid scheduler configuration")
