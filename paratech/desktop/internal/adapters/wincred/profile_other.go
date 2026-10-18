//go:build !windows

package wincred

import "path/filepath"

// realPath follows the symlinks; there are no junctions or short names off Windows.
func realPath(path string) (string, error) { return filepath.EvalSymlinks(path) }

// foldPath keeps the case: paths are case-sensitive off Windows.
func foldPath(path string) string { return path }
