package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/logfile"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/input/wails"
)

// Identity of the desktop application for the UI shell.
const (
	appName    = "Learning Assistant"
	instanceID = "paratech.learning-assistant.desktop"
)

// App is the composition root of the desktop core (ARCH-50).
//
// The core opens in two phases. openCore (synchronous, no goroutines) takes the single-instance guard, opens the
// storage and builds the use cases; the lifecycle then recovers the work of an earlier run and starts the background
// workers: with the UI shell once Wails runs (Options.Started), headless right after openCore. Close stops everything
// in reverse order.
type App struct {
	cfg config.Config
	log *slog.Logger
	// tee is the handler of log: the logger of main, and the log file once openCore holds the instance lock.
	tee *logfile.Tee
	env env
	// shell is nil in headless mode.
	shell *wails.Shell

	// startCtx ends when Close begins: it stops a startup that is still running.
	startCtx    context.Context
	cancelStart context.CancelFunc
	starting    sync.WaitGroup

	mu       sync.Mutex
	core     *core      // opened by openCore; nil until then
	life     *lifecycle // set with core
	launched bool       // the startup was launched
	closed   bool

	closeOnce sync.Once
	closeErr  error
}

// New validates the configuration and builds the application. It starts no goroutines and opens no windows.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		return nil, errors.New("app: logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Everything logs through the tee, the Wails logger behind its Info floor too, so the file gets what stderr gets.
	tee := logfile.NewTee(log.Handler())
	a := &App{cfg: cfg, log: slog.New(tee), tee: tee, env: productionEnv()}
	a.startCtx, a.cancelStart = context.WithCancel(context.Background())
	if !cfg.Headless && !cfg.Recorder {
		shell, err := wails.New(wails.Options{
			Name: appName, InstanceID: instanceID, Log: a.log, OpenCore: a.openCore, Started: a.launchStart,
		})
		if err != nil {
			return nil, fmt.Errorf("app: ui shell: %w", err)
		}
		a.shell = shell
	}
	return a, nil
}

// Run blocks until ctx is cancelled or, with the UI shell, until the user quits from the tray. In the recorder run
// mode it captures audio until its stdin closes (runRecorder).
// With the UI shell it must be called from the main goroutine (Wails owns the main OS thread). Headless it opens and
// starts the core itself and returns an error if that fails, for example ErrHeld of the instance lock.
func (a *App) Run(ctx context.Context) error {
	if a.cfg.Recorder {
		return a.runRecorder(ctx)
	}
	a.log.Info("desktop core started", "log_level", a.cfg.LogLevel, "headless", a.cfg.Headless)

	if a.shell == nil {
		if err := a.runHeadless(ctx); err != nil {
			return err
		}
	} else if err := a.shell.Run(ctx); err != nil {
		if errors.Is(err, wails.ErrUnsupported) {
			return fmt.Errorf("app: %w; run with --headless", err)
		}
		return fmt.Errorf("app: ui shell: %w", err)
	}

	a.log.Info("desktop core stopping")
	return nil
}

// runHeadless opens and starts the core, then waits for ctx. A stop during the startup is a normal stop.
func (a *App) runHeadless(ctx context.Context) error {
	if _, err := a.openCore(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	life, ok := a.claimStart(false)
	if !ok {
		return errors.New("app: closed")
	}
	if err := life.start(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	<-ctx.Done()
	return nil
}

// launchStart is the Started hook of the UI shell: it starts the core in the background, once, unless the core is
// not open or the app is closing. A failed start is logged; the window stays usable for settings and lists.
func (a *App) launchStart() {
	life, ok := a.claimStart(true)
	if !ok {
		return
	}
	go func() {
		defer a.starting.Done()
		if err := life.start(a.startCtx); err != nil && a.startCtx.Err() == nil {
			a.log.Error("background work did not start", "err", err)
		}
	}()
}

// claimStart returns the lifecycle to start, the first time and only while the core is open and the app not closing.
// A background startup is counted under the lock, so a Close that follows waits for it.
func (a *App) claimStart(background bool) (*lifecycle, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.life == nil || a.launched {
		return nil, false
	}
	a.launched = true
	if background {
		a.starting.Add(1)
	}
	return a.life, true
}

// openCore takes the single-instance guard of the data directory, then opens the database and the Storage Manager
// and builds the use cases the UI binds to. It starts nothing. With the UI shell it runs after Wails' own
// single-instance check (ADR-0007); the lock also guards against a headless instance or another build. Close releases
// what it opened.
func (a *App) openCore(ctx context.Context) (wails.Core, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.closed:
		return wails.Core{}, errors.New("app: closed")
	case a.core != nil:
		return wails.Core{}, errors.New("app: core is already open")
	case a.cfg.DataDir == "":
		return wails.Core{}, fmt.Errorf("app: no data directory: pass --data-dir or set %s", config.EnvDataDir)
	}
	// Windows ignores the 0o700 mode bits; privacy comes from the per-user %LOCALAPPDATA% ACL (PRD section 13.1).
	if err := os.MkdirAll(a.cfg.DataDir, 0o700); err != nil {
		return wails.Core{}, fmt.Errorf("app: create data directory: %w", err)
	}

	// First, before the database: a second instance must not recover the jobs and recordings of a live one.
	lock, err := a.env.lock(a.cfg.DataDir)
	if err != nil {
		return wails.Core{}, fmt.Errorf("app: single-instance guard: %w", err)
	}
	// Only the instance that holds the lock writes the log file; a second one must not mix into it.
	closeLog := a.openLogFile()
	// No path: it lies below the user's profile, and logs go into the support bundle.
	a.log.Info("instance lock acquired")
	if a.shell != nil {
		// Only the instance with the UI: the links bring its window forward, and a second instance never gets here.
		ensureURLScheme(a.env.scheme, os.Executable, a.log)
	}

	profile := a.secretsProfile()
	c, err := buildCore(ctx, a.cfg.DataDir, profile, a.env, a.log)
	if err != nil {
		return wails.Core{}, errors.Join(err, closeLog(), lock.Release())
	}
	if a.shell != nil {
		if err := c.wirePrompts(a.env, a.shell, a.log); err != nil {
			return wails.Core{}, errors.Join(err, c.close(), closeLog(), lock.Release())
		}
	}
	a.core = c
	a.life = c.newLifecycle(a.log, a.env.stopTimeout,
		closer{name: "storage", close: c.close}, closer{name: "log file", close: closeLog},
		closer{name: "instance lock", close: lock.Release})
	return c.ui, nil
}

// secretsProfile is the Credential Manager profile of the data directory, which exists by now (ADR-0014): the default
// names only for the default data dir, so a run on another dir never reads or changes the real secrets. The info line
// is what the smoke run of build-alpha.ps1 checks. The id hashes a predictable path with the user name in it, so like
// the paths it is only at debug level.
func (a *App) secretsProfile() wincred.Profile {
	profile := wincred.ProfileFor(a.cfg.DataDir, a.cfg.DefaultDataDir)
	if profile.Isolated() {
		a.log.Info("credential manager profile", "secrets_profile", "isolated")
		a.log.Debug("credential manager profile id", "secrets_profile_id", string(profile))
	} else {
		a.log.Info("credential manager profile", "secrets_profile", "default")
	}
	return profile
}

// openLogFile opens <DataDir>/logs/desktop.log and attaches it to the logger; it returns what detaches and closes it.
// A file that cannot be opened is a warning on the console, never a failed start.
func (a *App) openLogFile() func() error {
	none := func() error { return nil }
	if a.env.logFile == nil || a.tee == nil {
		return none
	}
	f, err := a.env.logFile(filepath.Join(a.cfg.DataDir, logsDir))
	if err != nil {
		a.log.Warn("the log file cannot be opened; logging to the console only", "err", err)
		return none
	}
	a.tee.Attach(f)
	return func() error {
		a.tee.Detach()
		return f.Close()
	}
}

// Close stops a running startup, then the background work, and releases the storage and the instance lock, in
// reverse order of creation. It is safe to call more than once and without Run.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		life := a.life
		a.mu.Unlock()

		a.cancelStart()
		a.starting.Wait()
		// The UI shell is torn down by Wails when Run returns.
		if life != nil {
			a.closeErr = life.shutdown()
		}
	})
	return a.closeErr
}
