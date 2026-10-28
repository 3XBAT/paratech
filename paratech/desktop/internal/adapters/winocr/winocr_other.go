//go:build !windows

package winocr

// newPlatform has no OCR engine off Windows: every Reader call returns ErrUnsupported.
func newPlatform() platform { return nil }
