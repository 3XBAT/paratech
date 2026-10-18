package wails

import (
	"embed"
	"io/fs"
)

// iconFile is the app icon (ADR-0015): a multi-size ICO of PNG images, 16 to 256 px. Wails picks the size for the tray
// and the window from it. desktop/build/windows/icon.ico, which the installer uses, is the same file (a test checks it).
const iconFile = "icon/paratech.ico"

// iconFiles is read-only embedded data fixed at build time, like assetFiles (ARCH-53).
//
//go:embed icon/paratech.ico
var iconFiles embed.FS

// appIcon returns a fresh copy of the icon, so no caller can change the embedded bytes.
func appIcon() []byte {
	b, err := fs.ReadFile(iconFiles, iconFile)
	if err != nil {
		panic(err) // unreachable: the file is embedded at build time
	}
	return b
}
