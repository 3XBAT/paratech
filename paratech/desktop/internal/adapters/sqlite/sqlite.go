package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	msqlite "modernc.org/sqlite"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
)

// Defaults of Options.
const (
	DefaultBusyTimeout  = 5 * time.Second
	DefaultMaxOpenConns = 4
)

// Options configures Open.
type Options struct {
	// Path is the database file. It must name a file (not ":memory:" or a "file:" URI: every pooled connection would
	// get its own private database) and must not contain a question mark. The directory must exist; the Storage
	// Manager owns the layout of the data directory.
	Path string

	// BusyTimeout is how long a connection waits for a lock held by another connection or process before the call
	// fails with ErrBusy. Zero means DefaultBusyTimeout. It is rounded down to whole milliseconds. SQLite does not
	// stop waiting when a context ends; DB.Do returns early on its own, other calls wait at most this long.
	BusyTimeout time.Duration

	// MaxOpenConns bounds the pool. Zero means DefaultMaxOpenConns. It must be at least 2: a function running in DB.Do
	// holds one connection, and a call made inside it with a context that does not carry the transaction needs
	// another one.
	MaxOpenConns int

	// Migrate applies pending migrations during Open.
	Migrate bool

	// Logger receives migration progress. Nil discards it.
	Logger *slog.Logger
}

// DB is the handle of the opened database: a connection pool configured for SQLite plus the transaction port. It is
// safe for concurrent use. Create it with Open and release it with Close.
type DB struct {
	sql    *sql.DB
	logger *slog.Logger

	// writeSlot (capacity 1) serializes the write transactions of this process. A writer takes it BEFORE it takes a
	// pool connection, so writers that wait do not hold connections and readers keep finding one (see Do).
	writeSlot chan struct{}

	// mu orders Close against the registration of abandoned BEGIN attempts in bg (see begin and Close).
	mu     sync.RWMutex
	closed atomic.Bool    // set under mu.Lock
	done   chan struct{}  // closed by Close; wakes writers that wait for the slot
	bg     sync.WaitGroup // BEGIN attempts that still run in the background
}

// buildDSN returns the driver DSN. Verified against modernc.org/sqlite v1.60.1 (see docs/refs/sqlite-goose-sqlc.md):
// a plain file name followed by a question mark and '&'-separated parameters; the query part is removed before the
// name reaches SQLite. The shorthand keys are applied in a fixed order by the driver on every new connection
// (busy_timeout first). _txlock=immediate makes the driver issue "begin immediate" for read-write transactions, which
// is what DB.Do relies on; it also protects goose, which starts its own transactions.
func buildDSN(path string, busy time.Duration) string {
	q := url.Values{}
	q.Set("_busy_timeout", strconv.FormatInt(busy.Milliseconds(), 10))
	q.Set("_foreign_keys", "1")
	q.Set("_journal_mode", "WAL")
	q.Set("_synchronous", "NORMAL")
	q.Set("_txlock", "immediate")
	return path + "?" + q.Encode()
}

// Open opens (and creates if missing) the database file, verifies that WAL and foreign keys are in effect and, when
// Options.Migrate is set, applies pending migrations. The caller must Close the handle.
func Open(ctx context.Context, opts Options) (*DB, error) {
	path := strings.TrimSpace(opts.Path)
	switch {
	case path == "":
		return nil, errors.New("sqlite: database path is empty")
	case strings.Contains(path, "?"):
		return nil, fmt.Errorf("sqlite: database path %q must not contain a question mark", path)
	case path == ":memory:" || strings.HasPrefix(path, "file:"):
		return nil, fmt.Errorf("sqlite: database path %q must be a plain file path", path)
	}
	busy := opts.BusyTimeout
	if busy == 0 {
		busy = DefaultBusyTimeout
	}
	if busy < time.Millisecond {
		return nil, fmt.Errorf("sqlite: busy timeout %v is below one millisecond", busy)
	}
	maxConns := opts.MaxOpenConns
	if maxConns == 0 {
		maxConns = DefaultMaxOpenConns
	}
	if maxConns < 2 {
		return nil, fmt.Errorf("sqlite: MaxOpenConns %d, need at least 2", maxConns)
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	connector, err := msqlite.NewConnector(buildDSN(path, busy))
	if err != nil {
		return nil, MapError(ctx, err)
	}
	pool := sql.OpenDB(connector)
	// Connections are long-lived on purpose: the pragmas are applied when a connection is created, and a desktop
	// app has no reason to recycle them.
	pool.SetMaxOpenConns(maxConns)
	pool.SetMaxIdleConns(maxConns)
	pool.SetConnMaxLifetime(0)

	db := &DB{sql: pool, logger: logger, writeSlot: make(chan struct{}, 1), done: make(chan struct{})}
	if err := db.verifyPragmas(ctx); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if opts.Migrate {
		if err := db.Migrate(ctx); err != nil {
			_ = pool.Close()
			return nil, err
		}
	} else if err := db.refuseNewerSchema(ctx); err != nil {
		// Without Migrate the schema is used as it is, so a database from a newer build must still be refused.
		_ = pool.Close()
		return nil, err
	}
	return db, nil
}

// verifyPragmas opens the first connection and checks that the settings that protect data are really in effect:
// SQLite silently keeps the old journal mode when it cannot switch (for example on some network file systems).
func (db *DB) verifyPragmas(ctx context.Context) error {
	var mode string
	if err := db.sql.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return MapError(ctx, err)
	}
	if !strings.EqualFold(mode, "wal") {
		return &Error{Kind: ErrDatabase, Detail: fmt.Sprintf("journal_mode is %q, WAL is required", mode)}
	}
	var fk int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return MapError(ctx, err)
	}
	if fk != 1 {
		return &Error{Kind: ErrDatabase, Detail: "foreign_keys is off"}
	}
	return nil
}

// Close closes all connections; the WAL files are checkpointed and removed when the last connection closes. It is
// safe to call more than once. Do returns ErrClosed afterwards.
//
// A Do that returned early because its context ended may still have a BEGIN waiting for a lock of another process
// (SQLite cannot be told to stop). Close waits for those attempts, at most one busy timeout, so that the connection
// they hold is released and the files are closed when Close returns.
func (db *DB) Close() error {
	db.mu.Lock()
	first := !db.closed.Swap(true)
	db.mu.Unlock()
	if !first {
		return nil
	}
	close(db.done)
	db.bg.Wait()
	return MapError(context.Background(), db.sql.Close())
}

// Queries returns the generated queries bound to the transaction carried by ctx, or to the pool when ctx carries
// none. Repositories call it once per method with the context they received.
func (db *DB) Queries(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(db.DBTX(ctx))
}
