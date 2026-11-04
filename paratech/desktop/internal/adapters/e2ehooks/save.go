package e2ehooks

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Names of a save dialog: the fallback of a name with nothing safe left, and the bound of a name in runes.
const (
	fallbackName = "file"
	maxNameRunes = 120
)

// SaveFile answers a save dialog with <dir>/saved/<name>, name reduced to a safe base name (safeName); saved is
// created when missing. An existing regular file there is replaced by the writer. "" (cancelled) when saved is not a
// plain folder or the target exists and is not a regular file; it never fails.
func (h *Hooks) SaveFile(_ context.Context, dialog, name string) (string, error) {
	path, err := h.savePath(name)
	if err != nil {
		h.log.Error(opSave, "dialog", dialog, "result", resultCancelled, "err", err)
		return "", nil
	}
	h.log.Info(opSave, "dialog", dialog, "name", filepath.Base(path))
	return path, nil
}

func (h *Hooks) savePath(name string) (string, error) {
	if !filepath.IsAbs(h.dir) {
		return "", errDirNotAbsolute
	}
	dir := filepath.Join(h.dir, savedDir)
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if err := plainDir(dir); err != nil {
		return "", errors.New(savedDir + ": " + err.Error())
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(realDir, safeName(name))
	if !below(realDir, target) {
		return "", errors.New("the name leaves " + savedDir)
	}
	fi, err := os.Lstat(target)
	switch {
	case err == nil && !fi.Mode().IsRegular():
		return "", errors.New("the target exists and is not a regular file")
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	return target, nil
}

// safeName is the base name of name (after its last '/' or '\') with control characters and the characters Windows
// does not allow (':' among them, so no stream) replaced by '_', no leading space, no trailing dot or space, at most
// maxNameRunes runes, a device name prefixed with '_', and fallbackName when nothing is left.
func safeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	n := 0
	for _, r := range name {
		if n == maxNameRunes {
			break
		}
		if r < 0x20 || r == 0x7f || r == '�' || strings.ContainsRune(forbiddenChars, r) {
			r = '_'
		}
		b.WriteRune(r)
		n++
	}
	s := strings.TrimLeft(strings.TrimRight(b.String(), ". "), " ")
	if s == "" {
		return fallbackName
	}
	if reservedName(s) {
		return "_" + s
	}
	return s
}
