package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"paratech/hub/config"
	"paratech/hub/internal/adapters/anthropic"
	"paratech/hub/internal/adapters/clock"
	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/adapters/random"
	"paratech/hub/internal/adapters/ratelimit"
	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/cases/quota"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/input/cli"
	"paratech/hub/internal/input/connect"
)

// HTTP server limits. There is no WriteTimeout: CompleteStream responses last as long as the model writes, and a
// write deadline would cut them off. ReadTimeout covers the whole request body (at most about 1 MiB).
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10
)

// App is the composition root of the hub (ARCH-50): config -> Postgres adapter -> use cases -> router and HTTP
// server, or the key-management CLI, as cfg.Command says.
type App struct {
	cfg config.Config
	log *slog.Logger
	out io.Writer // where the CLI prints its results, stdout by default

	llm   *llmParts   // built by New for serve only
	agent *agentParts // built by New for serve only

	mu      sync.Mutex
	started bool
	closed  bool
	db      *postgres.DB
}

// llmParts are the gateway's outbound parts that need no database: built and checked at New, so a bad allowlist or a
// missing provider key fails the start before anything is opened.
type llmParts struct {
	models   *modelallowlist.List
	provider *anthropic.Client
	limiter  *ratelimit.Limiter
}

// Option changes a default of New.
type Option func(*App)

// WithOutput sets where the key commands print their results (the plaintext key of "keys create" included). The
// default is stdout; the logger never receives these results.
func WithOutput(w io.Writer) Option {
	return func(a *App) { a.out = w }
}

// New validates the configuration and builds the application. It starts no goroutines and opens nothing. To serve
// it also needs the provider key and a valid model allowlist.
func New(cfg config.Config, log *slog.Logger, opts ...Option) (*App, error) {
	if log == nil {
		return nil, errors.New("app: logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, log: log, out: os.Stdout}
	for _, opt := range opts {
		opt(a)
	}
	if a.out == nil {
		return nil, errors.New("app: output is required")
	}
	if cfg.Command == config.CommandServe {
		parts, err := newLLMParts(cfg, log)
		if err != nil {
			return nil, err
		}
		a.llm = parts
		if a.agent, err = newAgentParts(cfg, parts.models, log); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// newLLMParts builds the allowlist (the domain rules of alias, model id and cap), the provider client and the rate
// limiter. Error messages never contain the provider key.
func newLLMParts(cfg config.Config, log *slog.Logger) (*llmParts, error) {
	if cfg.AnthropicAPIKey == "" {
		return nil, fmt.Errorf("app: %s is required to serve", config.EnvAnthropicAPIKey)
	}
	var entries []modelallowlist.Entry
	for _, m := range cfg.Models() {
		entries = append(entries, modelallowlist.Entry{Alias: m.Alias, Model: m.ID, MaxOutput: m.MaxOutput})
	}
	models, err := modelallowlist.New(entries)
	if err != nil {
		return nil, fmt.Errorf("app: %s: %w", config.EnvLLMModels, err)
	}
	provider, err := anthropic.New(anthropic.Options{APIKey: cfg.AnthropicAPIKey, BaseURL: cfg.AnthropicBaseURL, Timeout: cfg.LLMTimeout,
		Log: log})
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	rate, err := requestlimits.NewRate(cfg.RateLimitPerMinute, cfg.RateLimitBurst)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	limiter, err := ratelimit.New(rate)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return &llmParts{models: models, provider: provider, limiter: limiter}, nil
}

// Run executes cfg.Command: it serves until ctx is cancelled, or runs one key command and returns. Run may be called
// once; Close releases what it opened.
func (a *App) Run(ctx context.Context) error {
	if err := a.markStarted(); err != nil {
		return err
	}

	if a.cfg.Command == config.CommandServe {
		return a.runServer(ctx)
	}
	return a.runKeys(ctx)
}

// runServer listens, opens and migrates the database, then serves until ctx is cancelled and shuts the server down
// gracefully. A listen, database or serve failure is returned.
func (a *App) runServer(ctx context.Context) error {
	// Listen first, so that a busy port fails the start before the database is touched.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", a.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = ln.Close() }() // after a successful Serve the listener is already closed

	db, err := a.openDB(ctx)
	if err != nil {
		return err
	}

	auth, err := keyauth.New(db.APIKeys())
	if err != nil {
		return err
	}
	quotaSvc, err := quota.New(db.QuotaAccounts(), clock.System{}, random.Source{}, quota.Config{
		Unit:           a.cfg.QuotaUnit,
		Limit:          a.cfg.QuotaLimit,
		ReservationTTL: a.cfg.QuotaReservationTTL,
		KeyRetention:   a.cfg.IdempotencyRetention,
	})
	if err != nil {
		return err
	}
	quotaHandler, err := connect.NewQuota(quotaSvc, a.log)
	if err != nil {
		return err
	}
	gatewaySvc, err := gateway.New(gateway.Deps{
		Accounts: db.QuotaAccounts(), Results: db.QuotaAccounts(), Provider: a.llm.provider, Streamer: a.llm.provider,
		Limiter: a.llm.limiter, Clock: clock.System{}, Random: random.Source{}, Models: a.llm.models, Log: a.log,
	}, gateway.Config{
		Unit: a.cfg.QuotaUnit, Limit: a.cfg.QuotaLimit, ReservationTTL: a.cfg.QuotaReservationTTL,
		KeyRetention: a.cfg.IdempotencyRetention, CallTimeout: a.cfg.LLMTimeout, ServerPrompts: a.agent.serverPrompts,
		ProviderStream: a.cfg.ProviderStream,
	})
	if err != nil {
		return err
	}
	gatewayOpts := []connect.GatewayOption{connect.WithCompleteStream(gatewaySvc)}
	toPurge := purgers{gatewaySvc}
	if a.agent.sealer != nil {
		agentSvc, err := agentstep.New(agentstep.Deps{
			Accounts: db.QuotaAccounts(), Turns: db.AgentTurns(), Provider: a.llm.provider, Limiter: a.llm.limiter, Clock: clock.System{},
			Random: random.Source{}, Models: a.llm.models, Sealer: a.agent.sealer, Log: a.log,
		}, agentstep.Config{
			Unit: a.cfg.QuotaUnit, Limit: a.cfg.QuotaLimit, ReservationTTL: a.cfg.QuotaReservationTTL,
			KeyRetention: a.cfg.IdempotencyRetention, CallTimeout: a.cfg.LLMTimeout, Profiles: a.agent.profiles,
		})
		if err != nil {
			return err
		}
		gatewayOpts = append(gatewayOpts, connect.WithAgentStep(agentSvc))
		toPurge = append(toPurge, agentSvc)
	} else {
		a.log.Warn("agent steps disabled: no seal key", "env", config.EnvAgentSealKeys)
	}
	gatewayHandler, err := connect.NewGateway(gatewaySvc, a.log, gatewayOpts...)
	if err != nil {
		return err
	}
	handler, err := connect.NewRouter(connect.Deps{Log: a.log, Readiness: db, Auth: auth, Gateway: gatewayHandler, Quota: quotaHandler})
	if err != nil {
		return err
	}

	purgeCtx, stopPurge := context.WithCancel(ctx)
	purged := make(chan struct{})
	go func() {
		defer close(purged)
		purgeResults(purgeCtx, a.log, toPurge, account.ResultPurgeInterval)
	}()
	defer func() {
		stopPurge()
		<-purged
	}()

	a.log.Info("hub started", "log_level", a.cfg.LogLevel, "listen_addr", ln.Addr().String(), "provider_stream", a.cfg.ProviderStream)
	err = serve(ctx, a.log, newServer(handler, a.log), ln, a.cfg.ShutdownTimeout)
	a.log.Info("hub stopped")
	return err
}

// resultPurger is what the purge loop needs from the gateway and agent step use cases.
type resultPurger interface {
	PurgeResults(ctx context.Context) (int64, error)
}

// purgers purges each store of model output in turn; one failure does not stop the others.
type purgers []resultPurger

// PurgeResults implements resultPurger: the total count and every error.
func (ps purgers) PurgeResults(ctx context.Context) (int64, error) {
	var (
		total int64
		errs  []error
	)
	for _, p := range ps {
		n, err := p.PurgeResults(ctx)
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// purgeResults clears stored results past their result window now and then every interval until ctx ends (PRD
// section 13.2: no model output outlives the retention while the Hub runs).
func purgeResults(ctx context.Context, log *slog.Logger, svc resultPurger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		n, err := svc.PurgeResults(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			log.WarnContext(ctx, "purge of stored results failed", "err", err)
		case n > 0:
			log.InfoContext(ctx, "stored results purged", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runKeys runs one key command (PRD section 12) against the migrated database.
func (a *App) runKeys(ctx context.Context) error {
	db, err := a.openDB(ctx)
	if err != nil {
		return err
	}

	svc, err := keys.New(db.APIKeys(), clock.System{}, random.Source{})
	if err != nil {
		return err
	}
	cmd, err := cli.NewKeys(svc, a.out)
	if err != nil {
		return err
	}

	switch a.cfg.Command {
	case config.CommandKeysCreate:
		return cmd.Create(ctx, a.cfg.KeyLabel)
	case config.CommandKeysRevoke:
		return cmd.Revoke(ctx, a.cfg.KeyID)
	case config.CommandKeysList:
		return cmd.List(ctx)
	default:
		return fmt.Errorf("app: unknown command %s", a.cfg.Command)
	}
}

// openDB opens the database, hands it to Close and applies the migrations (idempotent).
func (a *App) openDB(ctx context.Context) (*postgres.DB, error) {
	db, err := postgres.Open(ctx, a.cfg.DatabaseURL, a.log)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := a.keepDB(db); err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	a.log.Info("database migrated")
	return db, nil
}

// Close releases resources in reverse order of creation. It is safe to call more than once and before Run.
func (a *App) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.closed = true
	if a.db != nil {
		a.db.Close()
		a.db = nil
	}
	return nil
}

func (a *App) markStarted() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case a.closed:
		return errors.New("app: closed")
	case a.started:
		return errors.New("app: Run called twice")
	}
	a.started = true
	return nil
}

// keepDB hands db over to Close. If Close already ran (a racing shutdown), db is closed here instead.
func (a *App) keepDB(db *postgres.DB) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		db.Close()
		return errors.New("app: closed")
	}
	a.db = db
	return nil
}

func newServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// serve runs srv on ln until ctx is cancelled, then shuts down: new connections are refused and in-flight requests
// get up to timeout to finish; after that the remaining connections are closed and an error is returned.
func serve(ctx context.Context, log *slog.Logger, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Info("hub stopping", "timeout", timeout)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		<-served
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
