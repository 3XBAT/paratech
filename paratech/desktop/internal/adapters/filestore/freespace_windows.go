//go:build windows

package filestore

import (
	"math"

	"golang.org/x/sys/windows"
)

// freeBytes asks GetDiskFreeSpaceExW for the bytes available to the caller (it honors disk quotas).
func freeBytes(dir string) (int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}

	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}
	if avail > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(avail), nil
}
