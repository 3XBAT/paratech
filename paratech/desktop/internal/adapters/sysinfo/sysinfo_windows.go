//go:build windows

package sysinfo

import (
	"strings"
	"unicode"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"paratech/desktop/internal/domain/recording/values"
)

// WindowsVersion is the domain value of the Windows version.
type WindowsVersion = values.WindowsVersion

// currentVersionKey holds the product texts of Windows; it is only read.
const currentVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// maxText bounds a registry text.
const maxText = 128

// windowsVersion is the Windows of this machine: the numbers from RtlGetVersion (no compatibility shims), the product
// texts and the update build revision from the registry, as the recorder child reports them (audiocapture). A value
// that cannot be read stays empty.
func windowsVersion() WindowsVersion {
	v := windows.RtlGetVersion()
	out := WindowsVersion{Major: v.MajorVersion, Minor: v.MinorVersion, Build: v.BuildNumber}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, currentVersionKey, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer func() { _ = k.Close() }()
	if s, _, err := k.GetStringValue("ProductName"); err == nil {
		out.Product = clean(s)
	}
	if s, _, err := k.GetStringValue("DisplayVersion"); err == nil {
		out.DisplayVersion = clean(s)
	}
	if n, _, err := k.GetIntegerValue("UBR"); err == nil && n <= 1<<32-1 {
		out.UBR = uint32(n)
	}
	return out
}

// clean drops control characters and bounds the text.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxText {
		s = string(r[:maxText])
	}
	return s
}

// profileDir asks Windows for the profile of the process token (no environment variable).
func profileDir() string {
	dir, err := windows.GetCurrentProcessToken().GetUserProfileDirectory()
	if err != nil {
		return ""
	}
	return dir
}
