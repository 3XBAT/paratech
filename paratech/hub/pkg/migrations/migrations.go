package migrations

import (
	"embed"
	"io/fs"
)

// files is read-only embedded data fixed at build time, not mutable state (ARCH-53); it has to be a package-level
// variable because go:embed requires it.
//
//go:embed *.sql
var files embed.FS

// FS returns the migration files at the root of the returned file system, which is the layout goose expects.
func FS() fs.FS {
	return files
}
