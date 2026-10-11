package config

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// Environment variable names read by Load.
const (
	EnvLogLevel = "LA_LOG_LEVEL"
	EnvDataDir  = "LA_DATA_DIR"
)

// Config is the validated desktop configuration.
type Config struct {
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// DataDir is the root of local application data. Empty means "choose the default later";
	// when set it must be an absolute path.
	DataDir string
}

// Default returns the configuration used when nothing is overridden.
func Default() Config {
	return Config{LogLevel: "info"}
}

// Load reads the configuration from the environment through getenv (os.Getenv in production,
// a map lookup in tests) and validates it. It is the only place that reads the environment (ARCH-52).
func Load(getenv func(string) string) (Config, error) {
	cfg := Default()
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvDataDir); v != "" {
		cfg.DataDir = v
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
	if c.DataDir != "" && !filepath.IsAbs(c.DataDir) {
		errs = append(errs, fmt.Errorf("data dir %q must be an absolute path", c.DataDir))
	}
	return errors.Join(errs...)
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
