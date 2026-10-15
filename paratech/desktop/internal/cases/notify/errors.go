package notify

import "errors"

// ErrInvalidConfig is returned by New when a port is missing or the job types are unusable.
var ErrInvalidConfig = errors.New("invalid notification configuration")
