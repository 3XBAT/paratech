package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/config"
	"paratech/desktop/internal/input/wails"
)

// Identity of the desktop application for the UI shell.
const (
	appName    = "Learning Assistant"
	instanceID = "paratech.learning-assistant.desktop"
)

// App is the composition root of the desktop core. Adapters and use cases are added here by later tasks (ARCH-50).
type App struct {
	cfg config.Config
	log *slog.Logger
	// shell is nil in headless mode.
	shell *wails.Shell
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
		shell, err := wails.New(wails.Options{Name: appName, InstanceID: instanceID, Log: log})
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

// Close releases resources in reverse order of creation. It must stay safe to call more than once.
func (a *App) Close() error {
	// No resources yet; the UI shell is torn down by Wails when Run returns.
	return nil
}
