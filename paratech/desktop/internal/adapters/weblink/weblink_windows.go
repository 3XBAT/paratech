//go:build windows

package weblink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// startTimeout bounds explorer.exe: it hands the URL to the default browser and exits.
const startTimeout = 15 * time.Second

// start runs %WINDIR%\explorer.exe with arg (a checked link or our .url file) as its only argument. The path comes
// from GetWindowsDirectory, not from PATH or the working directory. explorer.exe exits with 1 even when the page
// opened, so its exit code is ignored.
func start(ctx context.Context, arg string) error {
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return fmt.Errorf("weblink: windows directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	// G204: a fixed executable; arg is a checked link or the path of our own .url file.
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "explorer.exe"), arg) //nolint:gosec // see above
	cmd.Dir = dir
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return fmt.Errorf("weblink: start explorer: %w", err)
	}
	return nil
}

// shortcutPrefix names the temporary .url files; shortcutTTL is how long one is kept for explorer.exe to read it.
const (
	shortcutPrefix = "la-link-"
	shortcutTTL    = time.Minute
)

// startShortcut writes rawURL (already checked: no CR, LF or other control bytes) to a new .url file in the user's
// temporary directory and opens it with explorer.exe, which hands it to the default browser. The file is removed
// after shortcutTTL; leftovers of an earlier run are swept first.
func startShortcut(ctx context.Context, rawURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := os.TempDir()
	// explorer.exe reads ',' and ';' as switch separators; a hand-set %TEMP% with them is refused.
	if strings.ContainsAny(dir, ",;\"") {
		return fmt.Errorf("%w: the temporary directory path has ',', ';' or a quote", ErrRefused)
	}
	sweepShortcuts(dir, time.Now())

	f, err := os.CreateTemp(dir, shortcutPrefix+"*.url")
	if err != nil {
		return fmt.Errorf("weblink: shortcut: %w", err)
	}
	path := f.Name()
	_, werr := f.WriteString("[InternetShortcut]\r\nURL=" + rawURL + "\r\n")
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("weblink: shortcut: %w", err)
	}
	time.AfterFunc(shortcutTTL, func() { _ = os.Remove(path) })

	return start(ctx, path)
}

// sweepShortcuts removes our .url files older than shortcutTTL, left by a run that quit before its timer fired.
func sweepShortcuts(dir string, now time.Time) {
	old, _ := filepath.Glob(filepath.Join(dir, shortcutPrefix+"*.url"))
	for _, p := range old {
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() && now.Sub(fi.ModTime()) > shortcutTTL {
			_ = os.Remove(p)
		}
	}
}
