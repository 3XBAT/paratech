package shared

import (
	"strings"
	"unicode"
)

// forbiddenNameChars are rejected in identifiers and artifact path segments. They are separators or characters
// that Windows forbids in file names, so a value that passes is also a safe single file-name segment on every
// platform the app runs on.
const forbiddenNameChars = `\/:*?<>|"`

// reservedDeviceNames are Windows device names. They cannot be used as file names, with or without an extension
// ("NUL", "nul.txt"), in any letter case.
var reservedDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// nameProblem returns why s cannot be a single file-name segment, or "" when it can. s must be non-empty.
// Separators are included in the forbidden set: use it on one segment, not on a whole path.
func nameProblem(s string) string {
	for _, r := range s {
		if unicode.IsControl(r) {
			return "contains a control character"
		}
		if strings.ContainsRune(forbiddenNameChars, r) {
			return "contains a forbidden character"
		}
	}
	if s == "." || s == ".." {
		return `is "." or ".."`
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
		return "ends with a dot or a space"
	}
	base, _, _ := strings.Cut(s, ".")
	if reservedDeviceNames[strings.ToLower(strings.TrimRight(base, " "))] {
		return "is a reserved device name"
	}
	return ""
}

// maxFileNameRunes bounds SafeFileName: far below the 255 UTF-16 units of NTFS, so a folder path still fits.
const maxFileNameRunes = 120

// SafeFileName turns free text (a lesson title) into a single Windows file-name segment: forbidden and control
// characters become "_", runs of spaces collapse, the result is cut to maxFileNameRunes, trailing dots and spaces are
// trimmed and a reserved device name gets a leading "_". It returns "" when nothing usable is left.
func SafeFileName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			r = ' '
		case unicode.IsControl(r) || strings.ContainsRune(forbiddenNameChars, r):
			r = '_'
		}
		if r == ' ' && (space || b.Len() == 0) {
			continue
		}
		space = r == ' '
		b.WriteRune(r)
	}

	out := []rune(b.String())
	if len(out) > maxFileNameRunes {
		out = out[:maxFileNameRunes]
	}
	name := strings.TrimRight(string(out), ". ")
	if name == "" {
		return ""
	}

	base, _, _ := strings.Cut(name, ".")
	if reservedDeviceNames[strings.ToLower(strings.TrimRight(base, " "))] {
		name = "_" + name
	}
	return name
}
