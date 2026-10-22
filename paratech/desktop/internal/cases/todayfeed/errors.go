package todayfeed

import "errors"

// ErrInvalidConfig means a required port is missing. Storage errors and shared.ErrNotFound pass through wrapped.
var ErrInvalidConfig = errors.New("todayfeed: invalid configuration")
