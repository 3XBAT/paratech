package wails

import (
	"embed"
	"io/fs"
)

// Page paths served by the shell's asset handler.
const (
	mainPage  = "/"
	popupPage = "/popup.html"
)

// assetFiles is read-only embedded data fixed at build time, not mutable state (ARCH-53); go:embed requires a
// package-level variable. The real frontend replaces these pages in TASK-021.
//
//go:embed assets
var assetFiles embed.FS

// assets returns the pages at the root of the file system, the layout the Wails asset server expects.
func assets() fs.FS {
	sub, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err) // unreachable: "assets" is a valid path embedded above
	}
	return sub
}
