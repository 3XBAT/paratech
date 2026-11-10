package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
)

// Migrating a new database takes seconds under -race (the SQLite of modernc is instrumented Go), and most tests here
// open a core on a fresh data directory: migrating each one pushed the package past go test's 10 min default on CI.
// So a database file migrated once per test process is copied in first; buildCore still runs Migrate on it, which
// finds it up to date. The same as openAt in adapters/sqlite.
var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// migratedTemplate returns the contents of a freshly migrated database file. Close checkpoints the WAL and removes the
// side files, so the main file alone is the whole database.
func migratedTemplate() ([]byte, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "app-db-template-")
		if err != nil {
			templateErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path := filepath.Join(dir, dbFile)
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

// seedDatabase writes the migrated template as the database of dataDir unless one exists there or dataDir is missing
// (openCore creates it, and the test of that must see it so). It reports with t.Errorf: openCore may run on another
// goroutine than the test.
func seedDatabase(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, dbFile)
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if _, err := os.Stat(dataDir); err != nil {
		return
	}
	data, err := migratedTemplate()
	if err != nil {
		t.Errorf("migrated template: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Errorf("seed database: %v", err)
	}
}
