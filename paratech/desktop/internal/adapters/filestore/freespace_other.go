//go:build !windows && !linux && !darwin

package filestore

import "errors"

// freeBytes is not implemented on this OS; a download then cannot check the disk and refuses to start.
func freeBytes(string) (int64, error) { return 0, errors.ErrUnsupported }
