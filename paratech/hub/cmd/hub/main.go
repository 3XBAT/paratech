// Command hub is the entry point of the Learning Assistant hub.
// It only parses flags, loads configuration, builds the logger and a signal-aware context, and calls app (ARCH-51).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"paratech/hub/config"
	"paratech/hub/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	check := fs.Bool("check", false, "validate the configuration, build the application and exit")
	logLevel := fs.String("log-level", "", "log level: debug, info, warn, error (env "+config.EnvLogLevel+")")
	listenAddr := fs.String("listen", "", "host:port to listen on (env "+config.EnvListenAddr+")")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintln(stdout, version())
		return 0
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "config:", err)
		return 2
	}
	// Flags override the environment.
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if err := cfg.Validate(); err != nil {
		_, _ = fmt.Fprintln(stderr, "config:", err)
		return 2
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.Level()}))
	a, err := app.New(cfg, log)
	if err != nil {
		log.Error("build application", "err", err)
		return 1
	}
	defer func() {
		if err := a.Close(); err != nil {
			log.Error("close application", "err", err)
		}
	}()
	if *check {
		return 0
	}
	if err := a.Run(ctx); err != nil {
		log.Error("run application", "err", err)
		return 1
	}
	return 0
}

// version reports the module version embedded by the Go toolchain (no mutable globals, ARCH-53).
func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
