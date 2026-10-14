// Package wails is the Windows UI shell of the desktop core: the main window, the tray icon and the recording
// popup (PRD section 6.1, AC-01), built on Wails v3 (ADR-0001, ADR-0007).
//
// Wails code lives only in *_windows.go files (ARCH-33); on other systems Shell.Run returns ErrUnsupported, so the
// module builds and tests without CGO or a WebView. Bindings to use cases are added by later tasks (ARCH-40..44).
package wails
