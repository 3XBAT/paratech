package shared

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxAppNameLen bounds an app name; Windows file names have at most 255 characters.
const maxAppNameLen = 255

// appNameExt is the extension every app name ends with.
const appNameExt = ".exe"

// AppName identifies a desktop app across restarts by the file name of its executable, normalized to lower case, for
// example "chrome.exe" (TASK-059). The recording context records it and the settings context remembers it. The
// empty AppName means none.
type AppName string

// ParseAppName normalizes s (surrounding spaces, letter case) and validates it.
func ParseAppName(s string) (AppName, error) {
	a := AppName(strings.ToLower(strings.TrimSpace(s)))
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a, nil
}

// String returns the name as a plain string.
func (a AppName) String() string { return string(a) }

// IsZero reports whether no app is named.
func (a AppName) IsZero() bool { return a == "" }

// Validate reports whether a is a normalized executable file name: non-empty, at most 255 bytes of valid UTF-8, lower
// case, ending with ".exe", a single safe file-name segment (no path, no drive, no control characters).
func (a AppName) Validate() error {
	s := string(a)
	switch {
	case s == "":
		return fmt.Errorf("%w: empty", ErrInvalidAppName)
	case len(s) > maxAppNameLen:
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidAppName, maxAppNameLen)
	case !utf8.ValidString(s):
		return fmt.Errorf("%w: not valid UTF-8", ErrInvalidAppName)
	case strings.ToLower(s) != s:
		return fmt.Errorf("%w: not lower case", ErrInvalidAppName)
	case !strings.HasSuffix(s, appNameExt) || len(s) == len(appNameExt):
		return fmt.Errorf("%w: not an executable file name", ErrInvalidAppName)
	}
	if why := nameProblem(s); why != "" {
		return fmt.Errorf("%w: %s", ErrInvalidAppName, why)
	}
	return nil
}
