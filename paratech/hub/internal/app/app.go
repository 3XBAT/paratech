package app

import (
	"context"
	"errors"
	"log/slog"

	"paratech/hub/config"
)

// App is the composition root of the hub. It currently wires nothing: adapters, use cases and
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

// Run blocks until ctx is cancelled. No transport is served yet.
func (a *App) Run(ctx context.Context) error {
	a.log.Info("hub started", "log_level", a.cfg.LogLevel, "listen_addr", a.cfg.ListenAddr)
	<-ctx.Done()
	a.log.Info("hub stopping")
	return nil
}

// Close releases resources in reverse order of creation. It must stay safe to call more than once.
func (a *App) Close() error {
	// No resources yet.
	return nil
}
