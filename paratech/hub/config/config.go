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

	EnvQuotaUnit            = "HUB_QUOTA_UNIT"
	EnvQuotaLimit           = "HUB_QUOTA_LIMIT"
	EnvQuotaReservationTTL  = "HUB_QUOTA_RESERVATION_TTL"
	EnvIdempotencyRetention = "HUB_IDEMPOTENCY_RETENTION"
)

const maxShutdownTimeout = 5 * time.Minute

// Quota bounds (ADR-0008). The reservation TTL must exceed the longest gateway request (provider call plus
// settlement); the key retention is at most 24 hours (ADR-0005(d)).
const (
	QuotaUnitTokens         = "tokens"
	minReservationTTL       = time.Minute
	maxReservationTTL       = time.Hour
	minIdempotencyRetention = time.Hour
	maxIdempotencyRetention = 24 * time.Hour
)

// Command selects what the binary does. The zero value is CommandServe.
type Command int

// Commands: "hub [flags]" serves; "hub [flags] keys create|revoke|list ..." manages API keys (PRD section 12).
const (
	CommandServe Command = iota
	CommandKeysCreate
	CommandKeysRevoke
	CommandKeysList
)

// String returns the command line form of the command.
func (c Command) String() string {
	switch c {
	case CommandServe:
		return "serve"
	case CommandKeysCreate:
		return "keys create"
	case CommandKeysRevoke:
		return "keys revoke"
	case CommandKeysList:
		return "keys list"
	default:
		return fmt.Sprintf("Command(%d)", int(c))
	}
}

// Config is the validated hub configuration.
type Config struct {
	// Command is what to run; the fields below it that a command does not use are ignored by it.
	Command Command
	// KeyLabel is the label of the key that "keys create" issues (-label).
	KeyLabel string
	// KeyID is the id of the key that "keys revoke" revokes (-id).
	KeyID string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// ListenAddr is the host:port the hub will listen on (host may be empty, port is required).
	ListenAddr string
	// DatabaseURL is the Postgres URL (postgres://user:password@host:port/db?...). It is a secret: it is read only
	// from the environment (a flag would be visible in the process list) and never printed (see String, LogValue).
	DatabaseURL string
	// ShutdownTimeout bounds the graceful shutdown: in-flight requests get this long to finish.
	ShutdownTimeout time.Duration
	// QuotaUnit is the quota unit; only "tokens" for now (PRD section 19, decision 4; ADR-0008).
	QuotaUnit string
	// QuotaLimit is the allowance of every tester in QuotaUnit. It does not renew; a new value applies to everyone.
	QuotaLimit int64
	// QuotaReservationTTL is how long an unsettled reservation holds quota before the Hub releases it.
	QuotaReservationTTL time.Duration
	// IdempotencyRetention is how long the record of an idempotency key lives; after that the same key is new.
	IdempotencyRetention time.Duration
}

// Default returns the configuration used when nothing is overridden. It has no DatabaseURL, which is required.
func Default() Config {
	return Config{
		LogLevel: "info", ListenAddr: ":8080", ShutdownTimeout: 15 * time.Second,
		QuotaUnit: QuotaUnitTokens, QuotaLimit: 1_000_000, QuotaReservationTTL: 10 * time.Minute, IdempotencyRetention: 24 * time.Hour,
	}
}

// Load builds the configuration from defaults, then the environment (through getenv), then the command-line
// flags in args, and validates the result. It is the only place that reads the environment and flags (ARCH-52).
// Global flags come first; the remaining arguments select a command (see Command). For -h/--help it prints the
// usage to out and returns flag.ErrHelp.
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
	if v := getenv(EnvQuotaUnit); v != "" {
		cfg.QuotaUnit = v
	}
	if v := getenv(EnvQuotaLimit); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err != nil {
			envErrs = append(envErrs, fmt.Errorf("%s %q: not an integer", EnvQuotaLimit, v))
		} else {
			cfg.QuotaLimit = n
		}
	}
	for _, d := range []struct {
		env string
		dst *time.Duration
	}{{EnvQuotaReservationTTL, &cfg.QuotaReservationTTL}, {EnvIdempotencyRetention, &cfg.IdempotencyRetention}} {
		if v := getenv(d.env); v != "" {
			if parsed, err := time.ParseDuration(v); err != nil {
				envErrs = append(envErrs, fmt.Errorf("%s %q: not a duration (want for example 10m)", d.env, v))
			} else {
				*d.dst = parsed
			}
		}
	}

	// Flag defaults are the environment values, so a flag wins over the environment.
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error (env "+EnvLogLevel+")")
	fs.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "host:port to listen on (env "+EnvListenAddr+")")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout,
		"graceful shutdown limit (env "+EnvShutdownTimeout+"); the database URL is read only from env "+EnvDatabaseURL)
	fs.StringVar(&cfg.QuotaUnit, "quota-unit", cfg.QuotaUnit, "quota unit, only "+QuotaUnitTokens+" (env "+EnvQuotaUnit+")")
	fs.Int64Var(&cfg.QuotaLimit, "quota-limit", cfg.QuotaLimit, "quota of every tester in the quota unit, does not renew (env "+EnvQuotaLimit+")")
	fs.DurationVar(&cfg.QuotaReservationTTL, "quota-reservation-ttl", cfg.QuotaReservationTTL,
		"how long an unsettled reservation holds quota, 1m..1h, above the longest gateway request (env "+EnvQuotaReservationTTL+")")
	fs.DurationVar(&cfg.IdempotencyRetention, "idempotency-retention", cfg.IdempotencyRetention,
		"how long an idempotency key is remembered, 1h..24h (env "+EnvIdempotencyRetention+")")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(out, "Usage of "+programName+": "+programName+" [flags] [keys create -label L | keys revoke -id ID | keys list]")
			fs.SetOutput(out)
			fs.PrintDefaults()
		}

		return Config{}, err
	}
	if err := parseCommand(&cfg, fs.Args(), out); err != nil {
		return Config{}, err
	}

	if err := errors.Join(append(envErrs, cfg.Validate())...); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// parseCommand reads the command and its flags from the arguments left after the global flags.
func parseCommand(cfg *Config, args []string, out io.Writer) error {
	if len(args) == 0 {
		cfg.Command = CommandServe
		return nil
	}
	if args[0] != "keys" {
		return fmt.Errorf("unknown command %q (want keys)", args[0])
	}
	if len(args) < 2 {
		return errors.New("keys: want a subcommand: create, revoke or list")
	}

	fs := flag.NewFlagSet(programName+" keys "+args[1], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch args[1] {
	case "-h", "-help", "--help":
		_, _ = fmt.Fprintln(out, "Usage of "+programName+" keys: create -label L | revoke -id ID | list")
		return flag.ErrHelp
	case "create":
		cfg.Command = CommandKeysCreate
		fs.StringVar(&cfg.KeyLabel, "label", "", "pseudonymous label of the tester, 1-32 characters of a-z, 0-9, '-' and '_' (required)")
	case "revoke":
		cfg.Command = CommandKeysRevoke
		fs.StringVar(&cfg.KeyID, "id", "", "id of the key to revoke, as shown by keys list (required)")
	case "list":
		cfg.Command = CommandKeysList
	default:
		return fmt.Errorf("keys: unknown subcommand %q (want create, revoke or list)", args[1])
	}

	if err := fs.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(out, "Usage of "+fs.Name()+":")
			fs.SetOutput(out)
			fs.PrintDefaults()
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
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

// Validate reports every problem found, so a misconfigured start fails once with a complete message. The key
// commands do not serve, so the listen address, shutdown timeout and quota settings are checked only for serve; the
// label and id are checked for presence here and for their exact rules by the domain.
func (c Config) Validate() error {
	var errs []error
	if _, err := ParseLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}
	if err := validateDatabaseURL(c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}

	switch c.Command {
	case CommandServe:
		if err := validateListenAddr(c.ListenAddr); err != nil {
			errs = append(errs, err)
		}
		if c.ShutdownTimeout <= 0 || c.ShutdownTimeout > maxShutdownTimeout {
			errs = append(errs, fmt.Errorf("shutdown timeout %s: must be above 0 and at most %s", c.ShutdownTimeout, maxShutdownTimeout))
		}
		errs = append(errs, c.validateQuota()...)
	case CommandKeysCreate:
		if c.KeyLabel == "" {
			errs = append(errs, errors.New("keys create: -label is required"))
		}
	case CommandKeysRevoke:
		if c.KeyID == "" {
			errs = append(errs, errors.New("keys revoke: -id is required"))
		}
	case CommandKeysList:
	default:
		errs = append(errs, fmt.Errorf("unknown command %s", c.Command))
	}
	return errors.Join(errs...)
}

// validateQuota checks the quota settings, which only the server uses.
func (c Config) validateQuota() []error {
	var errs []error
	if c.QuotaUnit != QuotaUnitTokens {
		errs = append(errs, fmt.Errorf("quota unit %q: only %s is supported", c.QuotaUnit, QuotaUnitTokens))
	}
	if c.QuotaLimit <= 0 {
		errs = append(errs, fmt.Errorf("quota limit %d: must be above 0", c.QuotaLimit))
	}
	if c.QuotaReservationTTL < minReservationTTL || c.QuotaReservationTTL > maxReservationTTL {
		errs = append(errs, fmt.Errorf("quota reservation TTL %s: must be within %s..%s", c.QuotaReservationTTL, minReservationTTL, maxReservationTTL))
	}
	if c.IdempotencyRetention < minIdempotencyRetention || c.IdempotencyRetention > maxIdempotencyRetention {
		errs = append(errs, fmt.Errorf("idempotency retention %s: must be within %s..%s",
			c.IdempotencyRetention, minIdempotencyRetention, maxIdempotencyRetention))
	}
	return errs
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
	return fmt.Sprintf("{Command:%s LogLevel:%s ListenAddr:%s DatabaseURL:%s ShutdownTimeout:%s QuotaUnit:%s QuotaLimit:%d "+
		"QuotaReservationTTL:%s IdempotencyRetention:%s}",
		c.Command, c.LogLevel, c.ListenAddr, redacted(c.DatabaseURL), c.ShutdownTimeout, c.QuotaUnit, c.QuotaLimit,
		c.QuotaReservationTTL, c.IdempotencyRetention)
}

// LogValue is String for slog handlers that do not use fmt (JSON).
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("command", c.Command.String()),
		slog.String("log_level", c.LogLevel),
		slog.String("listen_addr", c.ListenAddr),
		slog.String("database_url", redacted(c.DatabaseURL)),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.String("quota_unit", c.QuotaUnit),
		slog.Int64("quota_limit", c.QuotaLimit),
		slog.Duration("quota_reservation_ttl", c.QuotaReservationTTL),
		slog.Duration("idempotency_retention", c.IdempotencyRetention),
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
