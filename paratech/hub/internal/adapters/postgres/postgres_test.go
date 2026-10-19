package postgres

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/pkg/migrations"
	"paratech/hub/testenv"
)

const marker = "s3cret-marker"

func TestOpenInvalidURL(t *testing.T) {
	_, err := Open(t.Context(), "postgres://hub:"+marker+"@host:port/hub", nil)
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("error = %v, want ErrInvalidURL", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestOpenUnreachable(t *testing.T) {
	// Port 1 on the loopback refuses connections; connect_timeout bounds a slow refusal.
	_, err := Open(t.Context(), "postgres://hub:"+marker+"@127.0.0.1:1/hub?sslmode=disable&connect_timeout=5", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestOpenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Open(ctx, "postgres://hub:"+marker+"@127.0.0.1:1/hub?sslmode=disable", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestTranslate(t *testing.T) {
	ctx := t.Context()
	if translate(ctx, ErrUnavailable, nil, marker) != nil {
		t.Fatal("nil must stay nil")
	}

	err := translate(ctx, ErrMigration, errors.New("server said "+marker), marker)
	if !errors.Is(err, ErrMigration) || strings.Contains(err.Error(), marker) {
		t.Fatalf("error = %v, want ErrMigration without the secret", err)
	}

	// A deadline of the driver's own connect timeout is a failure, not a cancelled call.
	err = translate(ctx, ErrUnavailable, context.DeadlineExceeded, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}

	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := translate(done, ErrUnavailable, context.Canceled, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// --- tests against a real database (skipped without HUB_TEST_DATABASE_URL) ------------------------

func openTestDB(t *testing.T, dbURL string) *DB {
	t.Helper()
	db, err := Open(t.Context(), dbURL, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func appliedVersions(t *testing.T, db *DB) []int64 {
	t.Helper()
	rows, err := db.pool.Query(t.Context(),
		"SELECT version_id FROM goose_db_version WHERE version_id > 0 AND is_applied ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// embeddedVersions returns the versions of the embedded migration files (NNNN_name.sql), in order.
func embeddedVersions(t *testing.T) []int64 {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS(), ".")
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		out = append(out, v)
	}
	// Pinned, so a broken embed (no files) cannot make the comparisons below pass vacuously.
	if len(out) == 0 || !slices.Contains(out, 1) || !slices.Contains(out, 2) || !slices.Contains(out, 3) || !slices.Contains(out, 4) ||
		!slices.Contains(out, 5) {
		t.Fatalf("embedded migration versions = %v, want at least 1 to 5", out)
	}
	return out
}

func TestEmbeddedVersions(t *testing.T) {
	embeddedVersions(t) // runs the pin without a database
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTestDB(t, testenv.PostgresURL(t))

	for i := 0; i < 2; i++ {
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate #%d: %v", i+1, err)
		}
	}
	if got, want := appliedVersions(t, db), embeddedVersions(t); !slices.Equal(got, want) {
		t.Fatalf("applied versions = %v, want %v", got, want)
	}

	// The version table lives in the test's own schema, not in public.
	var n int
	err := db.pool.QueryRow(t.Context(),
		"SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'goose_db_version'").Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("goose_db_version in current schema: n=%d err=%v", n, err)
	}

	if err := db.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestConcurrentMigrateAppliesOnce(t *testing.T) {
	dbURL := testenv.PostgresURL(t)
	dbs := []*DB{openTestDB(t, dbURL), openTestDB(t, dbURL), openTestDB(t, dbURL)}

	var wg sync.WaitGroup
	errs := make([]error, len(dbs))
	for i, db := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = db.Migrate(t.Context())
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Migrate #%d: %v", i+1, err)
		}
	}
	if got, want := appliedVersions(t, dbs[0]), embeddedVersions(t); !slices.Equal(got, want) {
		t.Fatalf("applied versions = %v, want %v exactly once", got, want)
	}
}

func TestClosedDB(t *testing.T) {
	db := openTestDB(t, testenv.PostgresURL(t))
	db.Close()
	db.Close() // idempotent

	if err := db.Ping(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ping after Close = %v, want ErrClosed", err)
	}
	if err := db.Migrate(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Migrate after Close = %v, want ErrClosed", err)
	}
}

func TestOpenWrongPassword(t *testing.T) {
	u, err := url.Parse(testenv.PostgresURL(t))
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(u.User.Username(), marker)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err = Open(ctx, u.String(), nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("error leaks the password: %v", err)
	}
}
