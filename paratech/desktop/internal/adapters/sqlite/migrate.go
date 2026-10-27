package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"paratech/desktop/pkg/migrations"
)

// provider builds a goose provider over the embedded migrations of pkg/migrations (ARCH-04: migrations live there, the
// adapter only runs them). goose.Provider.Close closes the *sql.DB it was given, so it is never called here.
//
// WithDisableGlobalRegistry: only the embedded SQL files are migrations, never Go migrations registered elsewhere in
// the process.
func (db *DB) provider() (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db.sql, migrations.FS(),
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
// ErrSchemaTooNew instead of being used with a schema this build does not know. Migrations are writes, so Migrate
// takes the write slot of Do and runs after the writers of this process that started before it.
func (db *DB) Migrate(ctx context.Context) error {
	if db.closed.Load() {
		return &Error{Kind: ErrClosed}
	}
	p, err := db.provider()
	if err != nil {
		return err
	}
	if err := db.refuseNewerSchema(ctx); err != nil {
		return err
	}
	if err := db.lockWriter(ctx); err != nil {
		return err
	}
	defer db.unlockWriter()
	// A NO TRANSACTION migration (0041) that fails midway can leave its pooled connection with foreign_keys OFF or
	// an open BEGIN; the DB is not used after that, because Open closes the pool when Migrate fails.
	if _, err := p.Up(ctx); err != nil {
		return MapError(ctx, fmt.Errorf("apply migrations: %w", err))
	}
	return nil
}

// SchemaVersion returns the highest applied migration version (0 for a database that was never migrated). It only
// reads: the goose version table is not created as a side effect.
func (db *DB) SchemaVersion(ctx context.Context) (int64, error) {
	if db.closed.Load() {
		return 0, &Error{Kind: ErrClosed}
	}
	return db.appliedVersion(ctx)
}

// appliedVersion reads the highest recorded version straight from goose's table (what goose's own GetDBVersion does)
// but without goose's side effect of creating the table, which a read-only check must not have.
func (db *DB) appliedVersion(ctx context.Context) (int64, error) {
	var n int
	err := db.sql.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version'").Scan(&n)
	if err != nil {
		return 0, MapError(ctx, err)
	}
	if n == 0 {
		return 0, nil
	}
	var v sql.NullInt64 // NULL for an empty table
	if err := db.sql.QueryRowContext(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&v); err != nil {
		return 0, MapError(ctx, err)
	}
	return v.Int64, nil
}

// refuseNewerSchema returns ErrSchemaTooNew when the database was migrated beyond the newest embedded migration. It
// does not write anything, so Open can call it also when Options.Migrate is false.
func (db *DB) refuseNewerSchema(ctx context.Context) error {
	p, err := db.provider()
	if err != nil {
		return err
	}
	var newest int64
	for _, src := range p.ListSources() {
		newest = max(newest, src.Version)
	}
	current, err := db.appliedVersion(ctx)
	if err != nil {
		return err
	}
	if current > newest {
		return &Error{Kind: ErrSchemaTooNew, Detail: fmt.Sprintf("database version %d, newest known %d", current, newest)}
	}
	return nil
}
