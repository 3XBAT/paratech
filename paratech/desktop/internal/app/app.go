package app

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/config"
)

// App is the composition root of the desktop core. It currently wires nothing: adapters, use cases and
// input are added here by later tasks (ARCH-50).
type App struct {
	cfg config.Config
	log *slog.Logger
}

// New validates the configuration and builds the application. It starts no goroutines.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		return nil, errors.New("app: logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &App{cfg: cfg, log: log}, nil
}

// Run blocks until ctx is cancelled. There is nothing to serve yet.
func (a *App) Run(ctx context.Context) error {
	a.log.Info("desktop core started", "log_level", a.cfg.LogLevel)
	<-ctx.Done()
	a.log.Info("desktop core stopping")
	return nil
}

// Close releases resources in reverse order of creation. It must stay safe to call more than once.
func (a *App) Close() error {
	// No resources yet.
	return nil
}
