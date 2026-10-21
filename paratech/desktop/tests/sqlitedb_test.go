package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
)

var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// openSQLite is sqlite.Open for the scenarios. When the database is to be migrated and its file does not exist yet,
// the file is first created as a copy of a database migrated once per test process (migrating takes seconds under
// -race); Open still runs Migrate on the copy and finds it up to date. An existing file is opened as it is.
func openSQLite(ctx context.Context, t testing.TB, opts sqlite.Options) (*sqlite.DB, error) {
	t.Helper()
	if opts.Migrate {
		if _, err := os.Stat(opts.Path); errors.Is(err, os.ErrNotExist) {
			data, err := migratedTemplate()
			if err != nil {
				t.Fatalf("migrated template: %v", err)
			}
			if err := os.WriteFile(opts.Path, data, 0o600); err != nil {
				_ = os.Remove(opts.Path) // for example a missing directory: Open reports it as before
			}
		}
	}
	return sqlite.Open(ctx, opts)
}

// migratedTemplate returns the contents of a freshly migrated database file. Close checkpoints the WAL and removes
// the side files, so the main file alone is the whole database.
func migratedTemplate() ([]byte, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sqlite-template-")
		if err != nil {
			templateErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path := filepath.Join(dir, "template.db")
		db, err := sqlite.Open(context.Background(), sqlite.Options{Path: path, Migrate: true})
		if err != nil {
			templateErr = err
			return
		}
		if err := db.Close(); err != nil {
			templateErr = err
			return
		}
		templateBytes, templateErr = os.ReadFile(path)
	})
	return templateBytes, templateErr
}
