//go:build linux || darwin

package filestore

import (
	"math"
	"syscall"
)

// freeBytes reads the blocks available to an unprivileged user from statfs.
func freeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}

	bsize := uint64(st.Bsize) //nolint:gosec // block size is positive
	if bsize != 0 && st.Bavail > math.MaxInt64/bsize {
		return math.MaxInt64, nil
	}
	return int64(st.Bavail * bsize), nil //nolint:gosec // bounded above
}
