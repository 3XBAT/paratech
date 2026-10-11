// Command desktop is the entry point of the Learning Assistant desktop core.
// It only loads the configuration, builds the logger and a signal-aware context, and calls app (ARCH-51).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"paratech/desktop/config"
	"paratech/desktop/internal/app"
)

func main() {
	cfg := config.MustLoad(os.Args[1:], os.Stderr, os.Getenv)

	log := setupLogger(cfg.Level())

	application, err := app.New(cfg, log)
	if err != nil {
		fatal(log, "build application", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = application.Run(ctx)
	stop()

	// Graceful shutdown: release resources even when Run failed.
	if closeErr := application.Close(); closeErr != nil {
		log.Error("close application", "err", closeErr)
	}
	if err != nil {
		fatal(log, "run application", err)
	}

	log.Info("gracefully stopped")
}

func setupLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// fatal logs err and terminates the process with a failure code.
func fatal(log *slog.Logger, msg string, err error) {
	log.Error(msg, "err", err)
	os.Exit(1)
}
