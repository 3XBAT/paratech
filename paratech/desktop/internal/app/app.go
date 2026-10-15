package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"paratech/desktop/config"
	"paratech/desktop/internal/adapters/clock"
	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/activity"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/input/wails"
)

// Identity of the desktop application for the UI shell.
const (
	appName    = "Learning Assistant"
	instanceID = "paratech.learning-assistant.desktop"
)

// dbFile is the SQLite database below the data directory.
const dbFile = "learning-assistant.db"

// App is the composition root of the desktop core (ARCH-50).
type App struct {
	cfg config.Config
	log *slog.Logger
	// shell is nil in headless mode.
	shell *wails.Shell
	// db is opened by openCore; nil until then.
	db *sqlite.DB
}

// New validates the configuration and builds the application. It starts no goroutines and opens no windows.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		return nil, errors.New("app: logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	a := &App{cfg: cfg, log: log}
	if !cfg.Headless {
		shell, err := wails.New(wails.Options{Name: appName, InstanceID: instanceID, Log: log, OpenCore: a.openCore})
		if err != nil {
			return nil, fmt.Errorf("app: ui shell: %w", err)
		}
		a.shell = shell
	}
	return a, nil
}

// Run blocks until ctx is cancelled or, with the UI shell, until the user quits from the tray.
// With the UI shell it must be called from the main goroutine (Wails owns the main OS thread).
func (a *App) Run(ctx context.Context) error {
	a.log.Info("desktop core started", "log_level", a.cfg.LogLevel, "headless", a.cfg.Headless)

	if a.shell == nil {
		<-ctx.Done()
	} else if err := a.shell.Run(ctx); err != nil {
		if errors.Is(err, wails.ErrUnsupported) {
			return fmt.Errorf("app: %w; run with --headless", err)
		}
		return fmt.Errorf("app: ui shell: %w", err)
	}

	a.log.Info("desktop core stopping")
	return nil
}

// openCore opens the database and builds the use cases the UI binds to. The shell calls it once from Run, after
// the single-instance check (ADR-0007); Close releases what it opened.
func (a *App) openCore(ctx context.Context) (wails.Core, error) {
	if a.db != nil {
		return wails.Core{}, errors.New("app: core is already open")
	}
	if a.cfg.DataDir == "" {
		return wails.Core{}, fmt.Errorf("app: no data directory: pass --data-dir or set %s", config.EnvDataDir)
	}
	// Windows ignores the 0o700 mode bits; privacy comes from the per-user %LOCALAPPDATA% ACL (PRD section 13.1).
	if err := os.MkdirAll(a.cfg.DataDir, 0o700); err != nil {
		return wails.Core{}, fmt.Errorf("app: create data directory: %w", err)
	}
	db, err := sqlite.Open(ctx, sqlite.Options{Path: filepath.Join(a.cfg.DataDir, dbFile), Migrate: true, Logger: a.log})
	if err != nil {
		return wails.Core{}, fmt.Errorf("app: open database: %w", err)
	}
	a.db = db

	prefs, err := preferences.New(sqlite.NewSettingsRepository(db), clock.System{})
	if err != nil {
		return wails.Core{}, fmt.Errorf("app: preferences: %w", err)
	}
	history, err := activity.New(sqlite.NewRecordingRepository(db), sqlite.NewJobRepository(db), a.log)
	if err != nil {
		return wails.Core{}, fmt.Errorf("app: activity: %w", err)
	}
	// No Stopper or Forcer yet: the app does not build the recorder or the job runner (later tasks wire them).
	return wails.Core{Settings: prefs, Activity: history}, nil
}

// Close releases resources in reverse order of creation. It must stay safe to call more than once.
func (a *App) Close() error {
	// The UI shell is torn down by Wails when Run returns.
	if a.db == nil {
		return nil
	}
	return a.db.Close() // safe to repeat
}
