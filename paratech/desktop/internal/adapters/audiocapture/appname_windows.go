//go:build windows && (amd64 || arm64)

package audiocapture

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The readable name of an app (TASK-082): FileDescription and ProductName of the version resource of its executable.

const (
	maxVersionInfo  = 1 << 20 // a larger version resource is not read
	maxVersionChars = 256     // longer strings are cut before they are cleaned
	maxTranslations = 8
)

// describe reads FileDescription and ProductName of the executable at path, in the first language of its translation
// table (then US English); empty strings when it has none or it cannot be read.
func (w *winApps) describe(path string) (description, product string) {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 || size > maxVersionInfo {
		return "", ""
	}
	buf := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buf[0])); err != nil { //nolint:gosec // G103: output buffer
		return "", ""
	}
	for _, lang := range translations(buf) {
		prefix := `\StringFileInfo\` + lang + `\`
		description, product = versionString(buf, prefix+"FileDescription"), versionString(buf, prefix+"ProductName")
		if description != "" || product != "" {
			return description, product
		}
	}
	return "", ""
}

// translations lists the language and code page keys of the version resource ("041904b0"), US English last.
func translations(buf []byte) []string {
	var out []string
	// The translation table is pairs of WORDs; its length is in bytes.
	if p, n, ok := versionValue(buf, `\VarFileInfo\Translation`, 1); ok {
		words := unsafe.Slice((*uint16)(p), n/2) //nolint:gosec // G103: inside buf, checked by versionValue
		for i := 0; i+1 < len(words) && len(out) < maxTranslations; i += 2 {
			out = append(out, fmt.Sprintf("%04x%04x", words[i], words[i+1]))
		}
	}
	return append(out, "040904b0", "040904e4")
}

// versionString reads a string value; empty when there is none. Its length is in characters.
func versionString(buf []byte, key string) string {
	p, n, ok := versionValue(buf, key, 2)
	if !ok || n == 0 {
		return ""
	}
	chars := unsafe.Slice((*uint16)(p), min(int(n), maxVersionChars)) //nolint:gosec // G103: inside buf, checked by versionValue
	return windows.UTF16ToString(chars)
}

// versionValue looks key up and checks that its n units of unit bytes lie inside buf.
func versionValue(buf []byte, key string, unit uintptr) (unsafe.Pointer, uint32, bool) {
	var (
		p unsafe.Pointer
		n uint32
	)
	if err := windows.VerQueryValue(unsafe.Pointer(&buf[0]), key, unsafe.Pointer(&p), &n); err != nil || p == nil { //nolint:gosec // G103: out-parameters
		return nil, 0, false
	}
	base, at := uintptr(unsafe.Pointer(&buf[0])), uintptr(p) //nolint:gosec // G103: a range check only
	if at < base || at-base > uintptr(len(buf)) || uintptr(n)*unit > uintptr(len(buf))-(at-base) {
		return nil, 0, false
	}
	return p, n, true
}
