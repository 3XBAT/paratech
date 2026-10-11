package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
)

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

// Load reads the configuration from the environment through getenv (os.Getenv in production,
// a map lookup in tests) and validates it. It is the only place that reads the environment (ARCH-52).
func Load(getenv func(string) string) (Config, error) {
	cfg := Default()
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvListenAddr); v != "" {
		cfg.ListenAddr = v
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
