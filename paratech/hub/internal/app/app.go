package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"paratech/hub/config"
	"paratech/hub/internal/adapters/postgres"
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

// App is the composition root of the hub (ARCH-50): config -> Postgres adapter -> router -> HTTP server.
type App struct {
	cfg config.Config
	log *slog.Logger

	mu      sync.Mutex
	started bool
	closed  bool
	db      *postgres.DB
}

// New validates the configuration and builds the application. It starts no goroutines and opens nothing.
func New(cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		return nil, errors.New("app: logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &App{cfg: cfg, log: log}, nil
}

// Run listens, opens and migrates the database, then serves until ctx is cancelled and shuts the server down
// gracefully. A listen, database or serve failure is returned. Run may be called once; Close releases what it opened.
func (a *App) Run(ctx context.Context) error {
	if err := a.markStarted(); err != nil {
		return err
	}

	// Listen first, so that a busy port fails the start before the database is touched.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", a.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = ln.Close() }() // after a successful Serve the listener is already closed

	db, err := postgres.Open(ctx, a.cfg.DatabaseURL, a.log)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if err := a.keepDB(db); err != nil {
		return err
	}
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	a.log.Info("database migrated")

	handler, err := connect.NewRouter(connect.Deps{Log: a.log, Readiness: db})
	if err != nil {
		return err
	}

	a.log.Info("hub started", "log_level", a.cfg.LogLevel, "listen_addr", ln.Addr().String())
	err = serve(ctx, a.log, newServer(handler, a.log), ln, a.cfg.ShutdownTimeout)
	a.log.Info("hub stopped")
	return err
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
