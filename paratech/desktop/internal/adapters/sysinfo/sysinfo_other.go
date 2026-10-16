//go:build !windows

package sysinfo

import "paratech/desktop/internal/domain/recording/values"

// WindowsVersion is the domain value of the Windows version.
type WindowsVersion = values.WindowsVersion

// windowsVersion is unknown outside Windows.
func windowsVersion() WindowsVersion { return WindowsVersion{} }

// profileDir is not looked up outside Windows (the product targets Windows only).
func profileDir() string { return "" }
