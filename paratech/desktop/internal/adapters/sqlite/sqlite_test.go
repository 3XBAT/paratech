package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// openTest opens a migrated database in a temporary directory. The cleanup closes it before the directory is removed
// (cleanups run last-in-first-out), which Windows requires: an open database file cannot be deleted.
func openTest(t *testing.T, mutate func(*Options)) *DB {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "test.db"), mutate)
}

// openAt opens the database at path. A missing file that is to be migrated is first created as a copy of a database
// migrated once per test process (migrating takes seconds under -race); Open still runs Migrate on it, which finds it
// up to date. Tests of the migrations themselves use openMigrating.
func openAt(t *testing.T, path string, mutate func(*Options)) *DB {
	t.Helper()
	opts := Options{Path: path, Migrate: true}
	if mutate != nil {
		mutate(&opts)
	}
	if opts.Migrate {
		seedFromTemplate(t, path)
	}
	db, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

// openMigrating opens a new database and really migrates it, bypassing the template: for the tests of migrations.
func openMigrating(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrating.db")
	db, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

var (
	templateOnce  sync.Once
	templateBytes []byte
	templateErr   error
)

// migratedTemplate returns the contents of a freshly migrated database file, built once per test process. Close
// checkpoints the WAL and removes the side files, so the main file alone is the whole database.
func migratedTemplate() ([]byte, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sqlite-template-")
		if err != nil {
			templateErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path := filepath.Join(dir, "template.db")
		db, err := Open(context.Background(), Options{Path: path, Migrate: true})
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

// seedFromTemplate writes the migrated template to path unless a database already exists there.
func seedFromTemplate(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return // an existing database is opened as it is
	}
	data, err := migratedTemplate()
	if err != nil {
		t.Fatalf("migrated template: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		_ = os.Remove(path) // for example a missing directory: Open reports it as before
	}
}

func mustExec(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()
	if _, err := db.sql.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func scanInt(t *testing.T, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := q.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func TestOpenAppliesPragmasOnEveryConnection(t *testing.T) {
	const conns = 3
	db := openTest(t, func(o *Options) {
		o.MaxOpenConns = conns
		o.BusyTimeout = 1234 * time.Millisecond
	})
	ctx := context.Background()

	// Hold all connections at once so that the pool has to create every one of them.
	held := make([]*sql.Conn, 0, conns)
	for i := 0; i < conns; i++ {
		c, err := db.sql.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn %d: %v", i, err)
		}
		held = append(held, c)
	}
	t.Cleanup(func() {
		for _, c := range held {
			_ = c.Close()
		}
	})

	for i, c := range held {
		var mode string
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Errorf("conn %d: journal_mode = %q, %v; want wal", i, mode, err)
		}
		for _, want := range []struct {
			pragma string
			value  int64
		}{
			{"foreign_keys", 1},
			{"busy_timeout", 1234},
			{"synchronous", 1}, // NORMAL
		} {
			var got int64
			if err := c.QueryRowContext(ctx, "PRAGMA "+want.pragma).Scan(&got); err != nil || got != want.value {
				t.Errorf("conn %d: %s = %d, %v; want %d", i, want.pragma, got, err, want.value)
			}
		}
	}
}

func TestOpenValidatesOptions(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		opts Options
	}{
		{"empty path", Options{}},
		{"question mark in path", Options{Path: filepath.Join(dir, "a?b.db")}},
		{"memory database", Options{Path: ":memory:"}},
		{"file uri", Options{Path: "file:" + filepath.ToSlash(filepath.Join(dir, "x.db"))}},
		{"pool of one", Options{Path: filepath.Join(dir, "one.db"), MaxOpenConns: 1}},
		{"sub-millisecond busy timeout", Options{Path: filepath.Join(dir, "busy.db"), BusyTimeout: time.Microsecond}},
		{"missing directory", Options{Path: filepath.Join(dir, "no-such-dir", "x.db")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(context.Background(), tc.opts)
			if err == nil {
				_ = db.Close()
				t.Fatal("Open succeeded, want an error")
			}
		})
	}
}

func TestOpenMapsDriverError(t *testing.T) {
	_, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "missing", "x.db")})
	if !errors.Is(err, ErrDatabase) {
		t.Fatalf("err = %v, want ErrDatabase", err)
	}
}

func TestOpenWithoutMigrateLeavesEmptyDatabase(t *testing.T) {
	db := openTest(t, func(o *Options) { o.Migrate = false })
	if n := scanInt(t, db.sql, "SELECT count(*) FROM sqlite_master WHERE type = 'table'"); n != 0 {
		t.Fatalf("%d tables without Migrate, want 0", n)
	}
	v, err := db.SchemaVersion(context.Background())
	if err != nil || v != 0 {
		t.Fatalf("SchemaVersion = %d, %v; want 0, nil", v, err)
	}
}

func TestCloseReleasesFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.db")
	seedFromTemplate(t, path)
	db, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO settings (id, updated_at) VALUES (1, 1)")
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("WAL file missing while open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	// The last connection checkpoints and removes the side files; deleting the database must work on Windows.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists after Close: %v", suffix, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove database after Close: %v", err)
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	seedFromTemplate(t, path)
	db, err := Open(context.Background(), Options{Path: path, Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO settings (id, updated_at) VALUES (1, 7)")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	again := openAt(t, path, nil) // Migrate: true on an up-to-date database
	if n := scanInt(t, again.sql, "SELECT updated_at FROM settings WHERE id = 1"); n != 7 {
		t.Fatalf("updated_at = %d after reopen, want 7", n)
	}
}
