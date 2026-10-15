package tests

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/config"
	"paratech/hub/internal/app"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// dummyProviderKey is an obviously fake provider key: serving needs one, these tests never call the provider.
const dummyProviderKey = "test-key-not-real"

// freeAddr returns a loopback address that was free a moment ago. The tiny race with other processes is accepted:
// a collision fails the test with a clear listen error.
func freeAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func status(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// TestServerAgainstPostgres starts the real Hub on a fresh schema: it migrates, serves the probes and the Connect
// routes, and stops gracefully on cancellation.
func TestServerAgainstPostgres(t *testing.T) {
	cfg := config.Default()
	cfg.DatabaseURL = testenv.PostgresURL(t)
	cfg.ListenAddr = freeAddr(t)
	cfg.AnthropicAPIKey = dummyProviderKey
	base := "http://" + cfg.ListenAddr

	a, err := app.New(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// Wait until the server answers (it starts serving after the migrations).
	deadline := time.Now().Add(30 * time.Second)
	for {
		code, err := status(t.Context(), base+"/healthz")
		if err == nil && code == http.StatusOK {
			break
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

	if code, err := status(t.Context(), base+"/readyz"); err != nil || code != http.StatusOK {
		t.Fatalf("readyz = %d, %v; want 200", code, err)
	}

	// Without an API key the Connect routes answer, and reject the call (the key flow is in keys_test.go).
	q := hubv1connect.NewQuotaServiceClient(http.DefaultClient, base)
	_, err = q.GetRemaining(t.Context(), connectrpc.NewRequest(&hubv1.GetRemainingRequest{}))
	if connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("GetRemaining error = %v, want unauthenticated", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil after cancellation", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
