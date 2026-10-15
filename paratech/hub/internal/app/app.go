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
	"paratech/hub/internal/adapters/clock"
	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/adapters/random"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
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

	mu      sync.Mutex
	started bool
	closed  bool
	db      *postgres.DB
}

// Option changes a default of New.
type Option func(*App)

// WithOutput sets where the key commands print their results (the plaintext key of "keys create" included). The
// default is stdout; the logger never receives these results.
func WithOutput(w io.Writer) Option {
	return func(a *App) { a.out = w }
}

// New validates the configuration and builds the application. It starts no goroutines and opens nothing.
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
	return a, nil
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
	handler, err := connect.NewRouter(connect.Deps{Log: a.log, Readiness: db, Auth: auth})
	if err != nil {
		return err
	}

	a.log.Info("hub started", "log_level", a.cfg.LogLevel, "listen_addr", ln.Addr().String())
	err = serve(ctx, a.log, newServer(handler, a.log), ln, a.cfg.ShutdownTimeout)
	a.log.Info("hub stopped")
	return err
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
