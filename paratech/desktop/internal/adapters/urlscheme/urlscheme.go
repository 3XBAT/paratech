// Package urlscheme keeps the per-user registration of the paratech: URL scheme (ADR-0013): the key
// HKCU\Software\Classes\paratech with the open command "<exe>" "%1". A paratech: link then starts this executable,
// which only brings the running app forward. No admin rights are needed and HKLM is never written.
package urlscheme

import (
	"errors"
	"fmt"
	"strings"
)

// Operation names of the errors.
const (
	opEnsure  = "urlscheme: ensure"
	opCommand = "urlscheme: command"
)

// The registration below HKEY_CURRENT_USER.
const (
	classesKey    = `Software\Classes\paratech`
	commandSubkey = `shell\open\command`
	description   = "URL:Paratech"
	protocolValue = "URL Protocol"
)

var (
	// ErrUnusablePath means the executable path cannot be put into the command: it is not an absolute Windows path,
	// or it holds a '"' (cannot be quoted), a '%' (the shell would expand %1 and the like inside it) or a control
	// character. The scheme then stays unregistered.
	ErrUnusablePath = errors.New("urlscheme: the executable path cannot be used in the open command")
	// ErrRegistry means the registry could not be read or written.
	ErrRegistry = errors.New("urlscheme: registry access failed")
)

// keys are the string values below HKEY_CURRENT_USER.
type keys interface {
	// value returns the REG_SZ value name of the key at path; ok is false when the key or the value is missing or
	// the value has another type.
	value(path, name string) (v string, ok bool, err error)
	// setValue writes the REG_SZ value, creating the key.
	setValue(path, name, v string) error
}

// Registrar keeps the registration. Create it with New.
type Registrar struct {
	keys keys
	root string // classesKey; tests use a key outside Software\Classes
}

// New returns the registrar of the current user's registry. Off Windows every Ensure fails with
// errors.ErrUnsupported.
func New() *Registrar { return &Registrar{keys: platformKeys{}, root: classesKey} }

// Ensure makes the scheme open exe: when any of the three values is missing or differs, it writes all of them. It
// reports whether it wrote. Errors never hold the path, which lies below the user's profile.
func (r *Registrar) Ensure(exe string) (bool, error) {
	cmd, err := Command(exe)
	if err != nil {
		return false, err
	}
	want := []struct{ path, name, value string }{
		{r.root, "", description},
		{r.root, protocolValue, ""},
		{r.root + `\` + commandSubkey, "", cmd},
	}

	same := true
	for _, w := range want {
		v, ok, err := r.keys.value(w.path, w.name)
		if err != nil {
			return false, fmt.Errorf("%s: %w: %w", opEnsure, ErrRegistry, err)
		}
		if !ok || v != w.value {
			same = false
			break
		}
	}
	if same {
		return false, nil
	}

	for _, w := range want {
		if err := r.keys.setValue(w.path, w.name, w.value); err != nil {
			return false, fmt.Errorf("%s: %w: %w", opEnsure, ErrRegistry, err)
		}
	}
	return true, nil
}

// Command is the open command of the scheme for exe: the quoted path and the quoted link, `"<exe>" "%1"`. The quotes
// keep a path with spaces and the whole link one argument each.
func Command(exe string) (string, error) {
	if !windowsAbs(exe) || strings.ContainsAny(exe, `"%`) || strings.ContainsFunc(exe, isControl) {
		return "", fmt.Errorf("%s: %w", opCommand, ErrUnusablePath)
	}
	return `"` + exe + `" "%1"`, nil
}

// windowsAbs reports a drive path (C:\...) or a UNC path (\\server\share\...), on any OS, so the tests run everywhere.
func windowsAbs(p string) bool {
	if strings.HasPrefix(p, `\\`) {
		return len(p) > 2
	}
	if len(p) < 4 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0] | 0x20 // lower case
	return c >= 'a' && c <= 'z'
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }
