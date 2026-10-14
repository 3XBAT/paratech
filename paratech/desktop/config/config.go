package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const programName = "desktop"

// Environment variable names read by Load.
const (
	EnvLogLevel = "LA_LOG_LEVEL"
	EnvDataDir  = "LA_DATA_DIR"
	EnvHeadless = "LA_HEADLESS"
	// EnvLocalAppData is the Windows per-user local application data directory, the base of the default DataDir.
	EnvLocalAppData = "LOCALAPPDATA"
)

// dataDirName is the directory of the application below EnvLocalAppData.
const dataDirName = "LearningAssistant"

// Config is the validated desktop configuration.
type Config struct {
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// DataDir is the root of local application data; when set it must be an absolute path. Load defaults it to
	// %LOCALAPPDATA%\LearningAssistant; it stays empty where that variable is unset (not Windows).
	DataDir string
	// Headless runs the core without the Windows UI shell (tray, windows), e.g. in tests and on other OSes.
	Headless bool
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
	if v := getenv(EnvLocalAppData); v != "" {
		cfg.DataDir = filepath.Join(v, dataDirName)
	}
	if v := getenv(EnvLogLevel); v != "" {
		cfg.LogLevel = v
	}
	if v := getenv(EnvDataDir); v != "" {
		cfg.DataDir = v
	}

	var envErr error
	if v := getenv(EnvHeadless); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			envErr = fmt.Errorf("%s=%q is not a boolean", EnvHeadless, v)
		}
		cfg.Headless = b
	}

	// Flag defaults are the environment values, so a flag wins over the environment.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (env "+EnvLogLevel+")")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "absolute path of the local data directory (env "+EnvDataDir+", default below %"+EnvLocalAppData+"%)")
	fs.BoolVar(&cfg.Headless, "headless", cfg.Headless, "run without the UI shell: no tray and no windows (env "+EnvHeadless+")")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(out, "Usage of "+programName+":")
			fs.SetOutput(out)
			fs.PrintDefaults()
		}

		return Config{}, err
	}

	if err := errors.Join(envErr, cfg.Validate()); err != nil {
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
