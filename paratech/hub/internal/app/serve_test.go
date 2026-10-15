package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startServe runs serve with handler on a loopback port and returns the base URL, the cancel of serve's context and
// the channel with serve's result.
func startServe(t *testing.T, handler http.Handler, timeout time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- serve(ctx, log, newServer(handler, log), ln, timeout) }()
	return "http://" + ln.Addr().String(), cancel, done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return")
		return nil
	}
}

func fetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestServeStopsOnCancel(t *testing.T) {
	url, cancel, done := startServe(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi"))
	}), 5*time.Second)

	if body, err := fetch(t.Context(), url); err != nil || body != "hi" {
		t.Fatalf("GET = %q, %v", body, err)
	}
	cancel()
	if err := wait(t, done); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
	if _, err := fetch(t.Context(), url); err == nil {
		t.Fatal("server still answers after shutdown")
	}
}

func TestServeLetsInFlightRequestsFinish(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	url, cancel, done := startServe(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("finished"))
	}), 5*time.Second)

	got := make(chan string, 1)
	go func() {
		body, err := fetch(context.Background(), url)
		if err != nil {
			body = "error: " + err.Error()
		}
		got <- body
	}()
	<-entered
	cancel()

	select {
	case err := <-done:
		t.Fatalf("serve returned %v while a request was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	if body := <-got; body != "finished" {
		t.Fatalf("in-flight request got %q", body)
	}
	if err := wait(t, done); err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

func TestServeShutdownTimeout(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	url, cancel, done := startServe(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done(): // the forced close cancels the request
		}
	}), 50*time.Millisecond)

	go func() { _, _ = fetch(context.Background(), url) }()
	<-entered
	cancel()

	err := wait(t, done)
	if err == nil || !strings.Contains(err.Error(), "graceful shutdown") {
		t.Fatalf("serve = %v, want a shutdown timeout error", err)
	}
}

func TestServeReturnsServeErrors(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close() // Serve on a closed listener fails at once

	log := slog.New(slog.DiscardHandler)
	err = serve(t.Context(), log, newServer(http.NotFoundHandler(), log), ln, time.Second)
	if err == nil || !strings.Contains(err.Error(), "serve") {
		t.Fatalf("serve = %v, want a serve error", err)
	}
}
