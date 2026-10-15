package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const programName = "hub"

// Environment variable names read by Load.
const (
	EnvLogLevel        = "HUB_LOG_LEVEL"
	EnvListenAddr      = "HUB_LISTEN_ADDR"
	EnvDatabaseURL     = "HUB_DATABASE_URL"
	EnvShutdownTimeout = "HUB_SHUTDOWN_TIMEOUT"
)

const maxShutdownTimeout = 5 * time.Minute

// Config is the validated hub configuration.
type Config struct {
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// ListenAddr is the host:port the hub will listen on (host may be empty, port is required).
	ListenAddr string
	// DatabaseURL is the Postgres URL (postgres://user:password@host:port/db?...). It is a secret: it is read only
	// from the environment (a flag would be visible in the process list) and never printed (see String, LogValue).
	DatabaseURL string
	// ShutdownTimeout bounds the graceful shutdown: in-flight requests get this long to finish.
	ShutdownTimeout time.Duration
}

// Default returns the configuration used when nothing is overridden. It has no DatabaseURL, which is required.
func Default() Config {
	return Config{LogLevel: "info", ListenAddr: ":8080", ShutdownTimeout: 15 * time.Second}
}

// Load builds the configuration from defaults, then the environment (through getenv), then the command-line
// flags in args, and validates the result. It is the only place that reads the environment and flags (ARCH-52).
// For -h/--help it prints the usage to out and returns flag.ErrHelp.
func Load(args []string, out io.Writer, getenv func(string) string) (Config, error) {
	cfg := Default()
	var envErrs []error
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvListenAddr); v != "" {
		cfg.ListenAddr = v
	}
	cfg.DatabaseURL = getenv(EnvDatabaseURL)
	if v := getenv(EnvShutdownTimeout); v != "" {
		if d, err := time.ParseDuration(v); err != nil {
			envErrs = append(envErrs, fmt.Errorf("%s %q: not a duration (want for example 15s)", EnvShutdownTimeout, v))
		} else {
			cfg.ShutdownTimeout = d
		}
	}

	// Flag defaults are the environment values, so a flag wins over the environment.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (env "+EnvLogLevel+")")
	fs.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "host:port to listen on (env "+EnvListenAddr+")")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout,
		"graceful shutdown limit (env "+EnvShutdownTimeout+"); the database URL is read only from env "+EnvDatabaseURL)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(out, "Usage of "+programName+":")
			fs.SetOutput(out)
			fs.PrintDefaults()
		}

		return Config{}, err
	}

	if err := errors.Join(append(envErrs, cfg.Validate())...); err != nil {
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
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}
	if c.ShutdownTimeout <= 0 || c.ShutdownTimeout > maxShutdownTimeout {
		errs = append(errs, fmt.Errorf("shutdown timeout %s: must be above 0 and at most %s", c.ShutdownTimeout, maxShutdownTimeout))
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

// validateDatabaseURL checks the shape of the URL only; connecting is the adapter's job. The messages never contain
// the URL or the url package's error, because both may carry the password.
func validateDatabaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%s is required (postgres://user:password@host:port/database)", EnvDatabaseURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: not a valid URL", EnvDatabaseURL)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("%s: scheme must be postgres or postgresql", EnvDatabaseURL)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%s: host is required", EnvDatabaseURL)
	}
	if strings.Trim(u.Path, "/") == "" {
		return fmt.Errorf("%s: database name is required", EnvDatabaseURL)
	}
	return nil
}

// String prints the configuration without the database URL, so that %v of a Config cannot leak the password.
func (c Config) String() string {
	return fmt.Sprintf("{LogLevel:%s ListenAddr:%s DatabaseURL:%s ShutdownTimeout:%s}",
		c.LogLevel, c.ListenAddr, redacted(c.DatabaseURL), c.ShutdownTimeout)
}

// LogValue is String for slog handlers that do not use fmt (JSON).
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("log_level", c.LogLevel),
		slog.String("listen_addr", c.ListenAddr),
		slog.String("database_url", redacted(c.DatabaseURL)),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
	)
}

func redacted(secret string) string {
	if secret == "" {
		return "(unset)"
	}
	return "(redacted)"
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
