//go:build windows

package weblink

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// startTimeout bounds explorer.exe: it hands the URL to the default browser and exits.
const startTimeout = 15 * time.Second

// start runs %WINDIR%\explorer.exe with arg as its only argument. The path comes from GetWindowsDirectory, not from
// PATH or the working directory. explorer.exe exits with 1 even when the page opened, so its exit code is ignored.
func start(ctx context.Context, arg string) error {
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return fmt.Errorf("weblink: windows directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	// G204: a fixed executable; arg passed checkLink, so syscall.EscapeArg leaves it as is.
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "explorer.exe"), arg) //nolint:gosec // see above
	cmd.Dir = dir
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return fmt.Errorf("weblink: start explorer: %w", err)
	}
	return nil
}
