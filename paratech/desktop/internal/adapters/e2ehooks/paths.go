package e2ehooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

// maxPathLen bounds a path of a pick.
const maxPathLen = 1024

// forbiddenChars are not allowed in a Windows file name; ':' also names a drive (C:foo) or a stream (a.wav:s).
const forbiddenChars = `<>:"|?*`

var (
	errDirNotAbsolute = errors.New("the e2e dir is not an absolute path")
	errNotPlainDir    = errors.New("not a plain folder (a link, a junction or another reparse point)")
)

// resolveFixture checks rel, a path of a pick, and returns the real path of the regular file it names below
// <dir>/fixtures. The check is lexical first (relative, no "..", no device name, stream, 8.3 name, trailing dot or
// space), then by Lstat of every component below fixtures (folders only, no reparse point, a regular file at the end),
// then by the resolved path, which must stay below the resolved fixtures. The file can still change after the check;
// returning the resolved path keeps that window small.
func (h *Hooks) resolveFixture(rel string) (string, error) {
	if !filepath.IsAbs(h.dir) {
		return "", errDirNotAbsolute
	}
	parts, err := relParts(rel)
	if err != nil {
		return "", err
	}
	if parts[0] != fixturesDir || len(parts) < 2 {
		return "", errors.New("not a file below " + fixturesDir + "/")
	}

	root := filepath.Join(h.dir, fixturesDir)
	if err := plainDir(root); err != nil {
		return "", fmt.Errorf("%s: %w", fixturesDir, err)
	}
	cur := root
	for i, p := range parts[1:] {
		cur = filepath.Join(cur, p)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", errors.New("component " + p + " does not exist")
		}
		if i == len(parts)-2 {
			if !fi.Mode().IsRegular() {
				return "", errors.New("not a regular file")
			}
		} else if fi.Mode().Type() != fs.ModeDir {
			return "", fmt.Errorf("component %s: %w", p, errNotPlainDir)
		}
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New(fixturesDir + " does not resolve")
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", errors.New("the path does not resolve")
	}
	if !below(realRoot, real) {
		return "", errors.New("the path resolves outside " + fixturesDir)
	}
	if fi, err := os.Stat(real); err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("the resolved path is not a regular file")
	}
	return real, nil
}

// relParts splits a relative path at '/' and '\' and checks every component.
func relParts(rel string) ([]string, error) {
	switch {
	case rel == "":
		return nil, errors.New("an empty path")
	case len(rel) > maxPathLen:
		return nil, fmt.Errorf("a path longer than %d bytes", maxPathLen)
	case !utf8.ValidString(rel):
		return nil, errors.New("a path that is not UTF-8")
	case rel[0] == '/' || rel[0] == '\\' || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "":
		return nil, errors.New("an absolute or rooted path")
	}
	for _, r := range rel {
		if r < 0x20 || r == 0x7f {
			return nil, errors.New("a control character")
		}
		if strings.ContainsRune(forbiddenChars, r) {
			return nil, fmt.Errorf("the character %q (a drive, a stream or not allowed by Windows)", r)
		}
	}
	parts := strings.Split(strings.ReplaceAll(rel, `\`, "/"), "/")
	for _, p := range parts {
		if err := checkComponent(p); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

func checkComponent(p string) error {
	switch {
	case p == "":
		return errors.New("an empty component (a doubled or trailing separator)")
	case p == "." || p == "..":
		return errors.New("a dot component")
	case strings.HasSuffix(p, ".") || strings.HasSuffix(p, " "):
		return errors.New("a component ending in a dot or a space")
	case strings.ContainsRune(p, '~'):
		return errors.New("a '~' (8.3 short names are not accepted)")
	case reservedName(p):
		return errors.New("a reserved device name")
	}
	return nil
}

// reservedName reports whether Windows reads name as a device: CON, PRN, AUX, NUL, COM0..9, LPT0..9 (also with the
// superscript digits), CONIN$, CONOUT$, with any extension and spaces before it.
func reservedName(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if rest, ok := strings.CutPrefix(stem, "COM"); ok {
		return portDigit(rest)
	}
	if rest, ok := strings.CutPrefix(stem, "LPT"); ok {
		return portDigit(rest)
	}
	return false
}

func portDigit(s string) bool {
	return len(s) == 1 && s[0] >= '0' && s[0] <= '9' || s == "¹" || s == "²" || s == "³"
}

// plainDir reports an error unless path is a folder that is no link, junction or other reparse point.
func plainDir(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return errors.New("does not exist")
	}
	if fi.Mode().Type() != fs.ModeDir {
		return errNotPlainDir
	}
	return nil
}

// below reports whether path lies strictly below root; both are resolved. Case is folded on Windows.
func below(root, path string) bool {
	r, p := foldCase(filepath.Clean(root)), foldCase(filepath.Clean(path))
	if !strings.HasSuffix(r, string(filepath.Separator)) {
		r += string(filepath.Separator)
	}
	return strings.HasPrefix(p, r) && len(p) > len(r)
}

func foldCase(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}
