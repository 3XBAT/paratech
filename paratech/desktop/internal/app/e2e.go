package app

// The E2E mode of the real app (ADR-0035 §3). config sets cfg.E2E only in a build with tag e2e; in every other build
// it is zero and nothing here changes the app.

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/e2ehooks"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/input/wails"
)

const (
	// e2eInstanceID is the Wails single-instance id of an E2E run: with the app's own id it would exit at once and
	// bring the owner's window forward.
	e2eInstanceID = instanceID + ".e2e"
	e2eTitleMark  = " · E2E"
	// e2eWebviewDir is the WebView2 user data folder below the control dir, never the owner's one.
	e2eWebviewDir = "webview"
)

// shellOptions are the name, identity, title, WebView2 folder and window of the UI shell; the E2E mode gives the
// shell its own. The hooks and the logger are set by New.
func shellOptions(cfg config.Config) wails.Options {
	o := wails.Options{Name: appName, InstanceID: instanceID, WebviewDataDir: cfg.WebviewDataDir}
	if !cfg.E2E.On() {
		return o
	}
	o.InstanceID = e2eInstanceID
	o.WebviewDataDir = e2eWebviewPath(cfg.E2E)
	o.Title = appName + e2eTitleMark
	if cfg.E2E.CDPPort != 0 {
		o.BrowserArgs = []string{"--remote-debugging-port=" + strconv.Itoa(cfg.E2E.CDPPort)}
	}
	o.MainWidth, o.MainHeight = cfg.E2E.WindowSize()
	return o
}

func e2eWebviewPath(e config.E2E) string { return filepath.Join(e.Dir, e2eWebviewDir) }

// e2eRecorder is the one e2ehooks adapter of an E2E run on the control dir: it answers the dialogs from its pick queue
// and records the links, files, call links and clipboard texts instead of acting. nil outside the mode.
func e2eRecorder(cfg config.Config, log *slog.Logger) *e2ehooks.Hooks {
	if !cfg.E2E.On() {
		return nil
	}
	return e2ehooks.New(cfg.E2E.Dir, log)
}

// e2eHooks are the shell's dialogs, clipboard and link and file openers of the recorder; without one they are zero,
// so the shell keeps the native ones.
func e2eHooks(h *e2ehooks.Hooks) wails.Hooks {
	if h == nil {
		return wails.Hooks{}
	}
	return wails.Hooks{Open: h, Save: h, Clipboard: h, Links: h, Opener: h}
}

// coreEnv is the env openCore builds the core with: in the E2E mode the Hub must be on loopback and the library's
// «Подключиться» goes to the recorder. Google's consent page stays native: ADR-0035 keeps Google out of E2E runs.
func (a *App) coreEnv() env {
	e := a.env
	e.loopbackHubOnly = a.cfg.E2E.On()
	if a.e2e != nil {
		e.calls = a.e2e
	}
	return e
}

// checkE2EDirs refuses an E2E run on the owner's folders: the data dir and the control dir must neither be, hold nor
// lie in the default data dir, and the run's WebView2 folder must stay apart from the owner's one. An unknown default
// data dir is refused too: then nothing can be told apart.
func checkE2EDirs(cfg config.Config) error {
	if !cfg.E2E.On() {
		return nil
	}
	var errs []error
	if cfg.DefaultDataDir == "" {
		errs = append(errs, errors.New("app: e2e: the default data dir is unknown (%"+config.EnvLocalAppData+"% is not set)"))
	} else {
		if overlaps(cfg.DataDir, cfg.DefaultDataDir) {
			errs = append(errs, errors.New("app: e2e: the data dir must be apart from the default data dir; pass --data-dir"))
		}
		if overlaps(cfg.E2E.Dir, cfg.DefaultDataDir) {
			errs = append(errs, errors.New("app: e2e: the e2e dir must be apart from the default data dir"))
		}
	}
	if overlaps(e2eWebviewPath(cfg.E2E), cfg.WebviewDataDir) {
		errs = append(errs, errors.New("app: e2e: the e2e dir must be apart from the WebView2 folder of the app"))
	}
	return errors.Join(errs...)
}

// checkE2EProfile refuses an E2E run whose existing data dir would open the default Credential Manager profile
// (ADR-0014): by file identity, which also catches what checkE2EDirs cannot see by name.
func checkE2EProfile(cfg config.Config) error {
	if cfg.E2E.On() && !wincred.ProfileFor(cfg.DataDir, cfg.DefaultDataDir).Isolated() {
		return errors.New("app: e2e: the data dir has the default credential profile")
	}
	return nil
}

// overlaps reports whether a and b are one directory or one lies below the other; false when either is empty.
func overlaps(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return within(a, b) || within(b, a)
}

// within reports whether path is dir or lies below it: by name (cleaned, so ".." and doubled separators do not
// matter; case folded on Windows) or, for the existing parts of path, by file identity (junctions, symlinks, 8.3
// names).
func within(path, dir string) bool {
	p, d := foldCase(filepath.Clean(path)), foldCase(filepath.Clean(dir))
	if p == d || strings.HasPrefix(p, withSeparator(d)) {
		return true
	}
	di, err := os.Stat(dir)
	if err != nil || !di.IsDir() {
		return false
	}
	for cur := filepath.Clean(path); ; {
		if fi, err := os.Stat(cur); err == nil && os.SameFile(fi, di) {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

func foldCase(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func withSeparator(dir string) string {
	if strings.HasSuffix(dir, string(filepath.Separator)) {
		return dir
	}
	return dir + string(filepath.Separator)
}
