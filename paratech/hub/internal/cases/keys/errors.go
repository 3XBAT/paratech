package keys

import "errors"

// ErrInvalidConfig means New got a nil port.
var ErrInvalidConfig = errors.New("keys: invalid configuration")
