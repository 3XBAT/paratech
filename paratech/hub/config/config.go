package config

import (
	"encoding/hex"
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

	EnvAnthropicAPIKey  = "HUB_ANTHROPIC_API_KEY" //nolint:gosec // G101: the name of the variable, not a credential
	EnvAnthropicBaseURL = "HUB_ANTHROPIC_BASE_URL"
	EnvLLMTimeout       = "HUB_LLM_TIMEOUT"
	EnvLLMModels        = "HUB_LLM_MODELS"
	EnvProviderStream   = "HUB_PROVIDER_STREAM"

	EnvRateLimitPerMinute = "HUB_RATE_LIMIT_PER_MINUTE"
	EnvRateLimitBurst     = "HUB_RATE_LIMIT_BURST"

	EnvAgentPlannerAlias = "HUB_AGENT_PLANNER_ALIAS"
	EnvAgentStrongAlias  = "HUB_AGENT_STRONG_ALIAS"
	EnvAgentSealKeys     = "HUB_AGENT_SEAL_KEYS" //nolint:gosec // G101: the name of the variable, not a credential
)

// LLM provider defaults and bounds. The timeout stays at most 10 minutes (the provider discourages longer
// non-streaming calls) and twice the timeout must fit in the reservation TTL, so the call and its settlement finish
// before the reservation expires.
const (
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
	DefaultLLMModels        = "default=claude-sonnet-5-5:8192,planner=claude-haiku-5-5:1024"
	minLLMTimeout           = 10 * time.Second
	maxLLMTimeout           = 10 * time.Minute
)

const maxShutdownTimeout = 5 * time.Minute

// Agent defaults and bounds (ADR-0023). The planner alias is the cheap model of AgentStep and INSPECT_MATERIAL, the
// strong alias the model of ANSWER and TRIAGE_ASSIGNMENT; both must be in the allowlist when they are used. A seal
// secret has at least 32 bytes, given as hex.
const (
	DefaultAgentPlannerAlias = "planner"
	DefaultAgentStrongAlias  = "default"
	minSealSecretBytes       = 32
	maxSealKeyVersion        = 255
)

// Rate limit bounds of the gateway, per tester (they mirror requestlimits.NewRate, which config may not import).
const (
	maxRateLimitPerMinute = 6000
	maxRateLimitBurst     = 1000
)

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
	// AnthropicAPIKey is the LLM provider key. It is a secret: read only from the environment and never printed.
	// Required to serve: app.New refuses to build the server without it; the key commands do not need it.
	AnthropicAPIKey string
	// AnthropicBaseURL is the provider API base URL: https, or http to a loopback host for local testing.
	AnthropicBaseURL string
	// LLMTimeout bounds one provider call.
	LLMTimeout time.Duration
	// LLMModels is the model allowlist, "alias=model-id:max_output_tokens[,...]" (see Models). Kept as text so that
	// Config stays comparable.
	LLMModels string
	// ProviderStream makes CompleteStream stream from the provider (ADR-0026). Off (the default), CompleteStream makes
	// one non-streaming call and sends the whole answer at once; the owner turns it on after the gateway probe passes.
	ProviderStream bool
	// RateLimitPerMinute is the average number of Complete calls a minute one API key may make.
	RateLimitPerMinute int
	// RateLimitBurst is how many calls one API key may make at once.
	RateLimitBurst int
	// AgentPlannerAlias is the allowlist alias of the agent planner (AgentStep) and of INSPECT_MATERIAL.
	AgentPlannerAlias string
	// AgentStrongAlias is the allowlist alias of ANSWER and TRIAGE_ASSIGNMENT.
	AgentStrongAlias string
	// AgentSealKeys are the secrets that seal the tool calls of AgentStep, "version:hex[,version:hex...]"; the first
	// signs, the others are still accepted (rotation). It is a secret: read only from the environment and never
	// printed. Empty disables AgentStep (unimplemented). See SealKeys.
	AgentSealKeys string
}

// SealKey is one parsed entry of AgentSealKeys.
type SealKey struct {
	Version byte
	Secret  []byte
}

// Model is one allowlist entry of LLMModels. Its alias and id rules are checked by the domain when the allowlist is
// built; config checks the syntax, the cap and duplicate aliases.
type Model struct {
	Alias     string
	ID        string
	MaxOutput int64
}

// Default returns the configuration used when nothing is overridden. It has no DatabaseURL, which is required.
func Default() Config {
	return Config{
		LogLevel: "info", ListenAddr: ":8080", ShutdownTimeout: 15 * time.Second,
		QuotaUnit: QuotaUnitTokens, QuotaLimit: 1_000_000, QuotaReservationTTL: 10 * time.Minute, IdempotencyRetention: 24 * time.Hour,
		AnthropicBaseURL: DefaultAnthropicBaseURL, LLMTimeout: 5 * time.Minute, LLMModels: DefaultLLMModels,
		RateLimitPerMinute: 30, RateLimitBurst: 10,
		AgentPlannerAlias: DefaultAgentPlannerAlias, AgentStrongAlias: DefaultAgentStrongAlias,
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
	for _, n := range []struct {
		env string
		dst *int
	}{{EnvRateLimitPerMinute, &cfg.RateLimitPerMinute}, {EnvRateLimitBurst, &cfg.RateLimitBurst}} {
		if v := getenv(n.env); v != "" {
			if parsed, err := strconv.Atoi(v); err != nil {
				envErrs = append(envErrs, fmt.Errorf("%s %q: not an integer", n.env, v))
			} else {
				*n.dst = parsed
			}
		}
	}
	cfg.AnthropicAPIKey = getenv(EnvAnthropicAPIKey)
	cfg.AgentSealKeys = getenv(EnvAgentSealKeys)
	if v := getenv(EnvAgentPlannerAlias); v != "" {
		cfg.AgentPlannerAlias = v
	}
	if v := getenv(EnvAgentStrongAlias); v != "" {
		cfg.AgentStrongAlias = v
	}
	if v := getenv(EnvAnthropicBaseURL); v != "" {
		cfg.AnthropicBaseURL = v
	}
	if v := getenv(EnvLLMModels); v != "" {
		cfg.LLMModels = v
	}
	if v := getenv(EnvProviderStream); v != "" {
		if b, err := strconv.ParseBool(v); err != nil {
			envErrs = append(envErrs, fmt.Errorf("%s %q: not a boolean (want true or false)", EnvProviderStream, v))
		} else {
			cfg.ProviderStream = b
		}
	}
	for _, d := range []struct {
		env string
		dst *time.Duration
	}{
		{EnvQuotaReservationTTL, &cfg.QuotaReservationTTL}, {EnvIdempotencyRetention, &cfg.IdempotencyRetention},
		{EnvLLMTimeout, &cfg.LLMTimeout},
	} {
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
	fs.StringVar(&cfg.AnthropicBaseURL, "anthropic-base-url", cfg.AnthropicBaseURL,
		"LLM provider API base URL, https or http to loopback (env "+EnvAnthropicBaseURL+"); the key is read only from env "+EnvAnthropicAPIKey)
	fs.DurationVar(&cfg.LLMTimeout, "llm-timeout", cfg.LLMTimeout,
		"limit of one provider call, 10s..10m, at most half the reservation TTL (env "+EnvLLMTimeout+")")
	fs.StringVar(&cfg.LLMModels, "llm-models", cfg.LLMModels,
		"model allowlist alias=model-id:max_output_tokens[,...] (env "+EnvLLMModels+")")
	fs.BoolVar(&cfg.ProviderStream, "provider-stream", cfg.ProviderStream,
		"stream answers from the provider in CompleteStream; off sends the whole answer at once (env "+EnvProviderStream+")")
	fs.IntVar(&cfg.RateLimitPerMinute, "rate-limit-per-minute", cfg.RateLimitPerMinute,
		"average Complete calls a minute per API key, 1..6000 (env "+EnvRateLimitPerMinute+")")
	fs.IntVar(&cfg.RateLimitBurst, "rate-limit-burst", cfg.RateLimitBurst,
		"Complete calls per API key at once, 1..1000 (env "+EnvRateLimitBurst+")")
	fs.StringVar(&cfg.AgentPlannerAlias, "agent-planner-alias", cfg.AgentPlannerAlias,
		"allowlist alias of the agent planner (env "+EnvAgentPlannerAlias+"); the seal keys are read only from env "+EnvAgentSealKeys)
	fs.StringVar(&cfg.AgentStrongAlias, "agent-strong-alias", cfg.AgentStrongAlias,
		"allowlist alias of the answer and triage operations (env "+EnvAgentStrongAlias+")")

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
// commands do not serve, so the listen address, shutdown timeout, quota, LLM and rate settings are checked only for serve; the
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
		errs = append(errs, c.validateLLM()...)
		if c.RateLimitPerMinute < 1 || c.RateLimitPerMinute > maxRateLimitPerMinute {
			errs = append(errs, fmt.Errorf("rate limit per minute %d: must be within 1..%d", c.RateLimitPerMinute, maxRateLimitPerMinute))
		}
		if c.RateLimitBurst < 1 || c.RateLimitBurst > maxRateLimitBurst {
			errs = append(errs, fmt.Errorf("rate limit burst %d: must be within 1..%d", c.RateLimitBurst, maxRateLimitBurst))
		}
		errs = append(errs, c.validateAgent()...)
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

// validateLLM checks the provider settings, which only the server uses. Messages never contain the key.
func (c Config) validateLLM() []error {
	var errs []error
	if c.AnthropicAPIKey != "" && !printableASCII(c.AnthropicAPIKey) {
		errs = append(errs, fmt.Errorf("%s: must be printable ASCII without spaces", EnvAnthropicAPIKey))
	}
	if err := validateBaseURL(c.AnthropicBaseURL); err != nil {
		errs = append(errs, err)
	}
	if c.LLMTimeout < minLLMTimeout || c.LLMTimeout > maxLLMTimeout {
		errs = append(errs, fmt.Errorf("LLM timeout %s: must be within %s..%s", c.LLMTimeout, minLLMTimeout, maxLLMTimeout))
	} else if 2*c.LLMTimeout > c.QuotaReservationTTL {
		errs = append(errs, fmt.Errorf("LLM timeout %s: must be at most half the quota reservation TTL %s, so a call and its settlement finish before the reservation expires",
			c.LLMTimeout, c.QuotaReservationTTL))
	}
	if _, err := ParseModels(c.LLMModels); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// validateAgent checks the agent settings, which only the server uses. Messages never contain a seal secret.
func (c Config) validateAgent() []error {
	var errs []error
	for _, a := range []struct{ name, value string }{{EnvAgentPlannerAlias, c.AgentPlannerAlias}, {EnvAgentStrongAlias, c.AgentStrongAlias}} {
		if !printableASCII(a.value) {
			errs = append(errs, fmt.Errorf("%s: must be non-empty printable ASCII without spaces", a.name))
		}
	}
	if _, err := c.SealKeys(); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// SealKeys parses AgentSealKeys: entries "version:hex" separated by commas, versions 1..255 and unique, secrets of at
// least 32 bytes. The first entry signs. Empty gives no keys (AgentStep disabled). Errors never quote a secret.
func (c Config) SealKeys() ([]SealKey, error) {
	if c.AgentSealKeys == "" {
		return nil, nil
	}
	var keys []SealKey
	seen := map[int]bool{}
	for i, entry := range strings.Split(c.AgentSealKeys, ",") {
		versionText, secretHex, ok := strings.Cut(strings.TrimSpace(entry), ":")
		version, err := strconv.Atoi(versionText)
		if !ok || err != nil || version < 1 || version > maxSealKeyVersion {
			return nil, fmt.Errorf("%s entry %d: want version:hex with a version of 1..%d", EnvAgentSealKeys, i+1, maxSealKeyVersion)
		}
		secret, err := hex.DecodeString(secretHex)
		if err != nil || len(secret) < minSealSecretBytes {
			return nil, fmt.Errorf("%s entry %d: the secret must be hex of at least %d bytes", EnvAgentSealKeys, i+1, minSealSecretBytes)
		}
		if seen[version] {
			return nil, fmt.Errorf("%s entry %d: version %d is repeated", EnvAgentSealKeys, i+1, version)
		}
		seen[version] = true
		keys = append(keys, SealKey{Version: byte(version), Secret: secret})
	}
	return keys, nil
}

// validateBaseURL accepts an https URL, or http to a loopback host (local fake provider), without user info, path,
// query or fragment. The adapter appends the fixed API path.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%s: must be an absolute URL such as %s", EnvAnthropicBaseURL, DefaultAnthropicBaseURL)
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname())) {
		return fmt.Errorf("%s: scheme must be https (http only to a loopback host)", EnvAnthropicBaseURL)
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("%s: must not contain user info, a path, a query or a fragment", EnvAnthropicBaseURL)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseModels parses an allowlist "alias=model-id:max_output_tokens[,alias=model-id:max_output_tokens...]". Alias and
// id must be printable ASCII without spaces; the cap must be an integer above 0; aliases must be unique.
func ParseModels(spec string) ([]Model, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, fmt.Errorf("%s: at least one model is required", EnvLLMModels)
	}
	var models []Model
	seen := map[string]bool{}
	for i, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		alias, rest, ok := strings.Cut(entry, "=")
		colon := strings.LastIndex(rest, ":")
		if !ok || colon < 0 {
			return nil, fmt.Errorf("%s entry %d: want alias=model-id:max_output_tokens", EnvLLMModels, i+1)
		}
		id, capText := rest[:colon], rest[colon+1:]
		if !printableASCII(alias) || !printableASCII(id) {
			return nil, fmt.Errorf("%s entry %d: alias and model id must be non-empty printable ASCII without spaces", EnvLLMModels, i+1)
		}
		maxOutput, err := strconv.ParseInt(capText, 10, 64)
		if err != nil || maxOutput <= 0 {
			return nil, fmt.Errorf("%s entry %d: max output tokens must be an integer above 0", EnvLLMModels, i+1)
		}
		if seen[alias] {
			return nil, fmt.Errorf("%s entry %d: duplicate alias %q", EnvLLMModels, i+1, alias)
		}
		seen[alias] = true
		models = append(models, Model{Alias: alias, ID: id, MaxOutput: maxOutput})
	}
	return models, nil
}

// Models returns the parsed allowlist of a validated configuration (nil if LLMModels is invalid).
func (c Config) Models() []Model {
	models, err := ParseModels(c.LLMModels)
	if err != nil {
		return nil
	}
	return models
}

func printableASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
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

// String prints the configuration without the database URL, the provider key and the seal keys, so that %v of a
// Config cannot leak them.
func (c Config) String() string {
	return fmt.Sprintf("{Command:%s LogLevel:%s ListenAddr:%s DatabaseURL:%s ShutdownTimeout:%s QuotaUnit:%s QuotaLimit:%d "+
		"QuotaReservationTTL:%s IdempotencyRetention:%s AnthropicAPIKey:%s AnthropicBaseURL:%s LLMTimeout:%s LLMModels:%s "+
		"ProviderStream:%t RateLimitPerMinute:%d RateLimitBurst:%d AgentPlannerAlias:%s AgentStrongAlias:%s AgentSealKeys:%s}",
		c.Command, c.LogLevel, c.ListenAddr, redacted(c.DatabaseURL), c.ShutdownTimeout, c.QuotaUnit, c.QuotaLimit,
		c.QuotaReservationTTL, c.IdempotencyRetention, redacted(c.AnthropicAPIKey), printableURL(c.AnthropicBaseURL), c.LLMTimeout, c.LLMModels,
		c.ProviderStream, c.RateLimitPerMinute, c.RateLimitBurst, c.AgentPlannerAlias, c.AgentStrongAlias, redacted(c.AgentSealKeys))
}

// GoString is String for %#v, which otherwise prints every field, secrets included.
func (c Config) GoString() string {
	return "config.Config" + c.String()
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
		slog.String("anthropic_api_key", redacted(c.AnthropicAPIKey)),
		slog.String("anthropic_base_url", printableURL(c.AnthropicBaseURL)),
		slog.Duration("llm_timeout", c.LLMTimeout),
		slog.String("llm_models", c.LLMModels),
		slog.Bool("provider_stream", c.ProviderStream),
		slog.Int("rate_limit_per_minute", c.RateLimitPerMinute),
		slog.Int("rate_limit_burst", c.RateLimitBurst),
		slog.String("agent_planner_alias", c.AgentPlannerAlias),
		slog.String("agent_strong_alias", c.AgentStrongAlias),
		slog.String("agent_seal_keys", redacted(c.AgentSealKeys)),
	)
}

// printableURL hides a base URL that could carry credentials (user info) or cannot be parsed.
func printableURL(raw string) string {
	if u, err := url.Parse(raw); err != nil || u.User != nil {
		return "(invalid)"
	}
	return raw
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
