package notice

import "errors"

// ErrInvalidNotice is returned by New and Restore for an inconsistent notice (ARCH-18).
var ErrInvalidNotice = errors.New("invalid job notice")
