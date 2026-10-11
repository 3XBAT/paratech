package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// The migrations are a build-time constant of the binary, so this is the one package-level variable of the adapter.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// provider builds a goose provider over the embedded migrations. goose.Provider.Close closes the *sql.DB it was given,
// so it is never called here.
//
// WithDisableGlobalRegistry: only the embedded SQL files are migrations, never Go migrations registered elsewhere in
// the process.
func (db *DB) provider() (*goose.Provider, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, &Error{Kind: ErrDatabase, Detail: err.Error()}
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db.sql, sub,
		goose.WithSlog(db.logger),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, &Error{Kind: ErrDatabase, Detail: err.Error()}
	}
	return p, nil
}

// Migrate applies all pending migrations. Running it on an up-to-date database changes nothing. A database whose
// recorded version is newer than the newest embedded migration (the user ran a newer build before) is refused with
// ErrSchemaTooNew instead of being used with a schema this build does not know.
func (db *DB) Migrate(ctx context.Context) error {
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}
	p, err := db.provider()
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return MapError(ctx, fmt.Errorf("apply migrations: %w", err))
	}
	current, target, err := p.GetVersions(ctx)
	if err != nil {
		return MapError(ctx, fmt.Errorf("read migration version: %w", err))
	}
	if current > target {
		return &Error{Kind: ErrSchemaTooNew, Detail: fmt.Sprintf("database version %d, newest known %d", current, target)}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version (0 for a database that was never migrated).
func (db *DB) SchemaVersion(ctx context.Context) (int64, error) {
	if db.closed.Load() {
		return 0, &Error{Kind: ErrClosed}
	}
	p, err := db.provider()
	if err != nil {
		return 0, err
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, MapError(ctx, err)
	}
	return v, nil
}
