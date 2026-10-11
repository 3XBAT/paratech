package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

const programName = "hub"

// Environment variable names read by Load.
const (
	EnvLogLevel   = "HUB_LOG_LEVEL"
	EnvListenAddr = "HUB_LISTEN_ADDR"
)

// Config is the validated hub configuration.
type Config struct {
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// ListenAddr is the host:port the hub will listen on (host may be empty, port is required).
	ListenAddr string
}

// Default returns the configuration used when nothing is overridden.
func Default() Config {
	return Config{LogLevel: "info", ListenAddr: ":8080"}
}

// Load builds the configuration from defaults, then the environment (through getenv), then the command-line
// flags in args, and validates the result. It is the only place that reads the environment and flags (ARCH-52).
// For -h/--help it prints the usage to out and returns flag.ErrHelp.
func Load(args []string, out io.Writer, getenv func(string) string) (Config, error) {
	cfg := Default()
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvListenAddr); v != "" {
		cfg.ListenAddr = v
	}

	// Flag defaults are the environment values, so a flag wins over the environment.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (env "+EnvLogLevel+")")
	fs.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "host:port to listen on (env "+EnvListenAddr+")")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(out, "Usage of "+programName+":")
			fs.SetOutput(out)
			fs.PrintDefaults()
		}

		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// MustLoad is Load for the program entry point: it exits instead of returning an error
// (status 0 for --help, 2 for a bad flag or configuration).
func MustLoad(args []string, out io.Writer, getenv func(string) string) Config {
	cfg, err := Load(args, out, getenv)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		_, _ = fmt.Fprintln(out, programName+": config:", err)
		os.Exit(2)
	}

	return cfg
}

// Validate reports every problem found, so a misconfigured start fails once with a complete message.
func (c Config) Validate() error {
	var errs []error
	if _, err := ParseLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}
	if err := validateListenAddr(c.ListenAddr); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func validateListenAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("listen address %q: port must be a number in 1..65535", addr)
	}
	return nil
}

// Level returns the slog level of a validated configuration.
func (c Config) Level() slog.Level {
	l, err := ParseLevel(c.LogLevel)
	if err != nil {
		return slog.LevelInfo
	}
	return l
}

// ParseLevel converts a textual log level to a slog level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", s)
	}
}
