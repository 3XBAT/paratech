//go:build windows

package fileopen

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// startTimeout bounds explorer.exe: it hands the file to the associated program and exits.
const startTimeout = 15 * time.Second

// launch starts %WINDIR%\explorer.exe with path as its only argument. The executable comes from GetWindowsDirectory,
// not from PATH or the working directory. Only a drive-letter path is handed on: explorer.exe reads a UNC or "\\?\"
// path differently, and past "C:" a colon would name an alternate data stream.
func (o *Opener) launch(ctx context.Context, path string) error {
	if !driveLetterPath(path) || strings.ContainsRune(path[2:], ':') {
		return errRefused
	}
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return fmt.Errorf("fileopen: windows directory: %w", err)
	}
	return o.run(ctx, filepath.Join(dir, "explorer.exe"), dir, path)
}

// driveLetterPath reports whether path starts with an ASCII drive letter, a colon and a backslash.
func driveLetterPath(path string) bool {
	if len(path) < 3 || path[1] != ':' || path[2] != '\\' {
		return false
	}
	c := path[0] | 0x20 // ASCII lower case
	return c >= 'a' && c <= 'z'
}

// runProcess runs exe with arg. explorer.exe exits with 1 even when the file opened, so its exit code is ignored.
func runProcess(ctx context.Context, exe, dir, arg string) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	// G204: a fixed executable from the Windows directory; arg is a checked path below the artifact root.
	cmd := exec.CommandContext(ctx, exe, arg) //nolint:gosec // see above
	cmd.Dir = dir
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return fmt.Errorf("fileopen: start explorer: %w", err)
	}
	return nil
}
