package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const programName = "desktop"

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

// Load builds the configuration from defaults, then the environment (through getenv), then the command-line
// flags in args, and validates the result. It is the only place that reads the environment and flags (ARCH-52).
// For -h/--help it prints the usage to out and returns flag.ErrHelp.
func Load(args []string, out io.Writer, getenv func(string) string) (Config, error) {
	cfg := Default()
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvDataDir); v != "" {
		cfg.DataDir = v
	}

	// Flag defaults are the environment values, so a flag wins over the environment.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (env "+EnvLogLevel+")")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "absolute path of the local data directory (env "+EnvDataDir+")")

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
