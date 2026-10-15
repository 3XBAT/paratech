package tests

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"paratech/hub/config"
	"paratech/hub/internal/app"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// keyPrefix is the prefix of issued keys (apikey.Prefix), repeated here so the test reads the CLI output as an
// operator would.
const keyPrefix = "pthub_"

// runKeyCommand runs one "hub keys ..." command against dbURL, like the binary does, and returns its stdout.
func runKeyCommand(t *testing.T, dbURL string, cmd config.Command, label, id string) string {
	t.Helper()
	cfg := config.Default()
	cfg.DatabaseURL = dbURL
	cfg.Command, cfg.KeyLabel, cfg.KeyID = cmd, label, id

	var out bytes.Buffer
	a, err := app.New(cfg, slog.New(slog.DiscardHandler), app.WithOutput(&out))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if err := a.Run(ctx); err != nil {
		t.Fatalf("%s: %v", cmd, err)
	}
	return out.String()
}

// startHub serves the Hub on a free loopback port until the test ends and returns its base URL.
func startHub(t *testing.T, dbURL string) string {
	t.Helper()
	cfg := config.Default()
	cfg.DatabaseURL = dbURL
	cfg.ListenAddr = freeAddr(t)
	base := "http://" + cfg.ListenAddr

	a, err := app.New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run = %v after cancellation", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("Run did not return after cancellation")
		}
		_ = a.Close()
	})

	deadline := time.Now().Add(30 * time.Second)
	for {
		code, err := status(t.Context(), base+"/healthz")
		if err == nil && code == http.StatusOK {
			return base
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned during start: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub did not become healthy: code=%d err=%v", code, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func getRemaining(t *testing.T, base, key string) error {
	t.Helper()
	req := connectrpc.NewRequest(&hubv1.GetRemainingRequest{})
	if key != "" {
		req.Header().Set("Authorization", "Bearer "+key)
	}
	_, err := hubv1connect.NewQuotaServiceClient(http.DefaultClient, base).GetRemaining(t.Context(), req)
	return err
}

func completeStream(t *testing.T, base, key string) error {
	t.Helper()
	req := connectrpc.NewRequest(&hubv1.CompleteStreamRequest{})
	req.Header().Set("Authorization", "Bearer "+key)
	stream, err := hubv1connect.NewGatewayServiceClient(http.DefaultClient, base).CompleteStream(t.Context(), req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	for stream.Receive() {
	}
	return stream.Err()
}

func requireInvalidKey(t *testing.T, err error) {
	t.Helper()
	var ce *connectrpc.Error
	if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeUnauthenticated || len(ce.Details()) != 1 {
		t.Fatalf("err = %v, want unauthenticated with one detail", err)
	}
	v, err := ce.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := v.(*hubv1.ErrorDetail); !ok || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY || d.GetRetryable() {
		t.Fatalf("detail = %v, want INVALID_API_KEY, not retryable", v)
	}
}

// issuedKey returns the single key line of the "keys create" output.
func issuedKey(t *testing.T, out string) string {
	t.Helper()
	var keys []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, keyPrefix) {
			keys = append(keys, line)
		}
	}
	if len(keys) != 1 || strings.Count(out, keys[0]) != 1 {
		t.Fatalf("want the key exactly once in the output:\n%s", out)
	}
	return keys[0]
}

// TestAPIKeyLifecycleAgainstPostgres is AC-25 end to end: a key issued by the CLI opens the protected RPCs, a call
// without it is rejected, a revoked key is rejected, and the database never holds the plaintext key.
func TestAPIKeyLifecycleAgainstPostgres(t *testing.T) {
	dbURL := testenv.PostgresURL(t)

	key := issuedKey(t, runKeyCommand(t, dbURL, config.CommandKeysCreate, "tester-01", ""))
	parts := strings.Split(key, "_")
	if len(parts) != 3 {
		t.Fatalf("issued key has %d parts", len(parts))
	}
	id, secret := parts[1], parts[2]

	base := startHub(t, dbURL)

	// With the key the call gets past authentication to the (not yet implemented) handlers.
	if err := getRemaining(t, base, key); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("GetRemaining with the key = %v, want unimplemented", err)
	}
	if err := completeStream(t, base, key); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("CompleteStream with the key = %v, want unimplemented", err)
	}
	requireInvalidKey(t, getRemaining(t, base, ""))

	// Revoke while the Hub runs: the next request is rejected.
	out := runKeyCommand(t, dbURL, config.CommandKeysRevoke, "", id)
	if !strings.Contains(out, "API key "+id+" revoked at") {
		t.Fatalf("revoke output: %q", out)
	}
	requireInvalidKey(t, getRemaining(t, base, key))
	requireInvalidKey(t, completeStream(t, base, key))

	out = runKeyCommand(t, dbURL, config.CommandKeysList, "", "")
	if !strings.Contains(out, id) || !strings.Contains(out, "tester-01") || !strings.Contains(out, "revoked") {
		t.Fatalf("list output:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Fatal("list prints the secret")
	}

	assertNoPlaintext(t, dbURL, key, secret)
}

// assertNoPlaintext reads every column of every api_keys row as text: neither the key nor its secret may appear.
func assertNoPlaintext(t *testing.T, dbURL, key, secret string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	rows, err := conn.Query(ctx, "SELECT t::text FROM api_keys t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		n++
		if strings.Contains(row, key) || strings.Contains(strings.ToLower(row), secret) {
			t.Fatalf("api_keys holds the plaintext key or secret: %s", row)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("api_keys has %d rows, want 1", n)
	}
}
