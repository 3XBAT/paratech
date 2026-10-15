package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnvTestDatabaseURL names the variable with the URL of a disposable Postgres database for DB tests, for example the
// one of hub/docker-compose.yml: postgres://hub:hub-dev-only@127.0.0.1:55432/hub?sslmode=disable.
const EnvTestDatabaseURL = "HUB_TEST_DATABASE_URL"

const schemaTimeout = 30 * time.Second

// PostgresURL returns a database URL whose search_path is a fresh schema private to t, so that test packages that
// run in parallel against one database do not see each other's tables (goose's version table included). The schema
// is dropped with everything in it when t ends. t is skipped when HUB_TEST_DATABASE_URL is not set.
func PostgresURL(t testing.TB) string {
	t.Helper()
	base := os.Getenv(EnvTestDatabaseURL)
	if base == "" {
		t.Skip(EnvTestDatabaseURL + " is not set; skipping a test that needs Postgres")
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(EnvTestDatabaseURL + " is not a valid URL") // the parser's error would quote the password
	}
	schema := "hubtest_" + randomHex(t)

	ctx, cancel := context.WithTimeout(context.Background(), schemaTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to %s: %v", EnvTestDatabaseURL, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), schemaTimeout)
		defer cancel()
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Errorf("drop schema %s: connect: %v", schema, err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})

	// pgx passes unknown URL parameters to the server as run-time parameters, so every connection of a pool opened
	// with this URL starts with this search_path.
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func randomHex(t testing.TB) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
