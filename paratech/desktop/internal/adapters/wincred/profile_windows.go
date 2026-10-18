//go:build windows

package wincred

import (
	"strings"

	"golang.org/x/sys/windows"
)

// Prefixes GetFinalPathNameByHandle puts before a drive path and a UNC path.
const (
	finalPathPrefix    = `\\?\`
	finalPathUNCPrefix = `\\?\UNC\`
)

// realPath asks Windows for the final path of the directory: junctions and symlinks followed, 8.3 short names
// expanded, a mapped drive shown as its UNC path. filepath.EvalSymlinks does not follow junctions.
func realPath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// No access rights needed for the name; FILE_FLAG_BACKUP_SEMANTICS lets CreateFile open a directory.
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // read-only handle, nothing to flush

	buf := make([]uint16, windows.MAX_PATH)
	for {
		// Flags 0: FILE_NAME_NORMALIZED | VOLUME_NAME_DOS.
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0) //nolint:gosec // len of a slice we made
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return stripFinalPrefix(windows.UTF16ToString(buf[:n])), nil
		}
		buf = make([]uint16, n) // n is the size needed, with the NUL
	}
}

func stripFinalPrefix(path string) string {
	switch {
	case strings.HasPrefix(path, finalPathUNCPrefix):
		return `\\` + path[len(finalPathUNCPrefix):]
	case strings.HasPrefix(path, finalPathPrefix):
		return path[len(finalPathPrefix):]
	}
	return path
}

// foldPath folds the case: Windows paths are case-insensitive.
func foldPath(path string) string { return strings.ToLower(path) }
