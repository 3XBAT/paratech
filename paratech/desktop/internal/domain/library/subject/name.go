package subject

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/library/values"
)

// MaxNameRunes bounds a subject name and an alias: the longest subject a schedule entry may name, so every schedule
// string can become one.
const MaxNameRunes = 200

// Name is a subject name or alias: one plain line of 1 to MaxNameRunes characters.
type Name struct{ text string }

// NewName cleans s to one line (values.CleanText) and checks its length.
func NewName(s string) (Name, error) {
	text := values.CleanText(s)
	if text == "" || utf8.RuneCountInString(text) > MaxNameRunes {
		return Name{}, fmt.Errorf("%w: %d characters", ErrInvalidName, utf8.RuneCountInString(text))
	}
	return Name{text: text}, nil
}

// String returns the name as shown.
func (n Name) String() string { return n.text }

// Key returns the matching key (values.Key).
func (n Name) Key() string { return values.Key(n.text) }

// MaxFolderNameRunes bounds a folder name.
const MaxFolderNameRunes = 64

// forbiddenFolderChars are separators and the characters Windows forbids in file names: a folder may be written to
// disk one day (an export), so its name already is a valid Windows file name.
const forbiddenFolderChars = `\/:*?"<>|`

// reservedDeviceNames are Windows device names, invalid as file names with or without an extension, in any case.
var reservedDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// FolderName is the name of a folder: 1 to MaxFolderNameRunes characters that are a valid Windows file name.
type FolderName struct{ text string }

// NewFolderName trims s and collapses its spaces, then checks it: no control or forbidden character, not "." or
// "..", no trailing dot, not a device name (CON, NUL, COM1, ... also with an extension).
func NewFolderName(s string) (FolderName, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return FolderName{}, fmt.Errorf("%w: control character", ErrInvalidFolderName)
		}
	}
	text := values.CleanText(s)
	switch n := utf8.RuneCountInString(text); {
	case n == 0:
		return FolderName{}, fmt.Errorf("%w: empty", ErrInvalidFolderName)
	case n > MaxFolderNameRunes:
		return FolderName{}, fmt.Errorf("%w: longer than %d characters", ErrInvalidFolderName, MaxFolderNameRunes)
	case strings.ContainsAny(text, forbiddenFolderChars):
		return FolderName{}, fmt.Errorf("%w: forbidden character", ErrInvalidFolderName)
	case strings.HasSuffix(text, "."):
		return FolderName{}, fmt.Errorf("%w: ends with a dot", ErrInvalidFolderName)
	}
	base, _, _ := strings.Cut(text, ".")
	if reservedDeviceNames[strings.ToLower(strings.TrimSpace(base))] {
		return FolderName{}, fmt.Errorf("%w: reserved device name", ErrInvalidFolderName)
	}
	return FolderName{text: text}, nil
}

// String returns the name as shown.
func (n FolderName) String() string { return n.text }

// Key returns the matching key (values.Key): two folders of a subject may not share it.
func (n FolderName) Key() string { return values.Key(n.text) }
