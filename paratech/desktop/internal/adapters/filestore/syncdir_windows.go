//go:build windows

package filestore

import "os"

// syncDir is a no-op: Windows cannot flush a directory handle, and NTFS journals the rename.
func syncDir(*os.Root, string) {}
