//go:build !windows

package filestore

import "os"

// syncDir flushes the directory entry of a renamed file; best effort.
func syncDir(root *os.Root, dir string) {
	d, err := root.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
