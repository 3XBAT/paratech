package wails

import (
	"embed"
	"errors"
	"io/fs"
	"strings"
)

// Page paths served by the shell's asset handler.
const (
	mainPage  = "/"
	popupPage = "/popup.html"
)

// Embedded pages. assets/ holds the committed pages: the recording popup and a fallback main page. The React app
// (desktop/frontend) is built by `npm run build` into assets/app/, which git ignores. go:embed cannot reach outside
// this directory and CI builds Go without npm, so a build without the frontend still compiles and shows the
// fallback page. Release builds run `npm run build`, then build and test with -tags production: a production-only
// test fails when the built app is missing.
const (
	assetsDir = "assets"
	appDir    = "assets/app"
)

// assetFiles is read-only embedded data fixed at build time, not mutable state (ARCH-53); go:embed requires a
// package-level variable. "all:" keeps files that Vite may name with a leading "_". The pattern cannot be narrowed to
// skip dotfiles: a pattern for assets/app matches nothing without a build, which is a compile error. layered.Open
// refuses dot-prefixed names instead.
//
//go:embed all:assets
var assetFiles embed.FS

// assets returns the pages at the root of the file system, the layout the Wails asset server expects: the built
// React app first, then the committed pages (popup, fallback main page).
func assets() fs.FS {
	return layered{mustSub(appDir), mustSub(assetsDir)}
}

func mustSub(dir string) fs.FS {
	sub, err := fs.Sub(assetFiles, dir)
	if err != nil {
		panic(err) // unreachable: dir is a valid constant path; a missing directory only makes Open fail
	}
	return sub
}

// layered opens a name from the first file system that has it. Names with a dot-prefixed element (stray dotfiles
// embedded by "all:") are not served.
type layered []fs.FS

func (l layered) Open(name string) (fs.File, error) {
	for _, elem := range strings.Split(name, "/") {
		if strings.HasPrefix(elem, ".") && elem != "." {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		}
	}
	for _, f := range l {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}
