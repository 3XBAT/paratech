package postgres

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"paratech/hub/pkg/migrations"
)

const (
	// defaultConnectTimeout bounds one connection attempt when the URL sets no connect_timeout, so that a
	// black-holed host fails the start instead of hanging until the OS gives up.
	defaultConnectTimeout = 10 * time.Second

	// Migration lock: another Hub instance migrating the same database is waited for, polling every second for up
	// to a minute.
	lockPollSeconds = 1
	lockPollTries   = 60
)

// DB is the Hub database: a pgx pool. It is safe for concurrent use.
type DB struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	secret string // the password, removed from error texts

	closed    atomic.Bool
	closeOnce sync.Once
}

// Open parses the database URL, creates the pool and checks that the database answers. It does not migrate.
func Open(ctx context.Context, databaseURL string, log *slog.Logger) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, &Error{Kind: ErrInvalidURL}
	}
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = defaultConnectTimeout
	}
	secret := cfg.ConnConfig.Password
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, translate(ctx, ErrUnavailable, err, secret)
	}

	db := &DB{pool: pool, log: log, secret: secret}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Ping reports whether the database answers. It is the readiness check of the Hub.
func (db *DB) Ping(ctx context.Context) error {
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}
	return translate(ctx, ErrUnavailable, db.pool.Ping(ctx), db.secret)
}

// Migrate applies all pending migrations of pkg/migrations. Running it on an up-to-date database changes nothing.
// A Postgres advisory lock serializes concurrent starts of several Hub instances.
//
// WithDisableGlobalRegistry: only the embedded SQL files are migrations, never Go migrations registered elsewhere.
func (db *DB) Migrate(ctx context.Context) error {
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}

	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(lockPollSeconds, lockPollTries))
	if err != nil {
		return translate(ctx, ErrMigration, err, db.secret)
	}

	// Closing the *sql.DB (done by the provider's Close) does not close the pool it borrows connections from.
	sqlDB := stdlib.OpenDBFromPool(db.pool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS(),
		goose.WithSlog(db.log),
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		_ = sqlDB.Close()
		return translate(ctx, ErrMigration, err, db.secret)
	}
	defer func() { _ = p.Close() }()

	if _, err := p.Up(ctx); err != nil {
		return translate(ctx, ErrMigration, err, db.secret)
	}
	return nil
}

// Close closes the pool. It is safe to call more than once.
func (db *DB) Close() {
	db.closeOnce.Do(func() {
		db.closed.Store(true)
		db.pool.Close()
	})
}
