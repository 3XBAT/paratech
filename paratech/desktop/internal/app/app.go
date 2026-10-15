package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"paratech/desktop/config"
	"paratech/desktop/internal/cases/notify"
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

	a := &App{cfg: cfg, log: log, env: productionEnv()}
	a.startCtx, a.cancelStart = context.WithCancel(context.Background())
	if !cfg.Headless {
		shell, err := wails.New(wails.Options{
			Name: appName, InstanceID: instanceID, Log: log, OpenCore: a.openCore, Started: a.launchStart,
		})
		if err != nil {
			return nil, fmt.Errorf("app: ui shell: %w", err)
		}
		a.shell = shell
	}
	return a, nil
}

// Run blocks until ctx is cancelled or, with the UI shell, until the user quits from the tray.
// With the UI shell it must be called from the main goroutine (Wails owns the main OS thread). Headless it opens and
// starts the core itself and returns an error if that fails, for example ErrHeld of the instance lock.
func (a *App) Run(ctx context.Context) error {
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
	// No path: it lies below the user's profile, and logs go into the support bundle.
	a.log.Info("instance lock acquired")

	// Toasts need the shell; a headless core tells nobody about finished jobs.
	var notifier notify.Notifier
	if a.shell != nil {
		notifier = a.shell
	}
	c, err := buildCore(ctx, a.cfg.DataDir, a.env, notifier, a.log)
	if err != nil {
		return wails.Core{}, errors.Join(err, lock.Release())
	}
	if a.shell != nil {
		if err := c.wirePrompts(a.env, a.shell, a.log); err != nil {
			return wails.Core{}, errors.Join(err, c.close(), lock.Release())
		}
	}
	a.core = c
	a.life = &lifecycle{
		log: a.log, runner: c.runner, recordings: c.recordings, deliveries: c.deliveries, cleanups: c.cleanups,
		loops:       c.loops(),
		closers:     []closer{{name: "storage", close: c.close}, {name: "instance lock", close: lock.Release}},
		stopTimeout: a.env.stopTimeout,
	}
	return c.ui, nil
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
