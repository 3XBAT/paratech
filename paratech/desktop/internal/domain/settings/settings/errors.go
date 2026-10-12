package settings

import "errors"

// Errors of the settings package (ARCH-18).
var (
	// ErrInvalidSettings is returned by Restore for an inconsistent stored state and by the change methods for a
	// malformed value or a missing change time.
	ErrInvalidSettings = errors.New("invalid settings")
)
