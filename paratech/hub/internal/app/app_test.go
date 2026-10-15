package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"paratech/hub/config"
	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/app"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// unreachableDB refuses connections (port 1 on the loopback) and fails fast thanks to connect_timeout.
const unreachableDB = "postgres://hub:s3cret-marker@127.0.0.1:1/hub?sslmode=disable&connect_timeout=5"

func validConfig(listen string) config.Config {
	cfg := config.Default()
	cfg.ListenAddr = listen
	cfg.DatabaseURL = unreachableDB
	return cfg
}

func TestNew(t *testing.T) {
	noDB := config.Default()
	tests := []struct {
		name    string
		cfg     config.Config
		log     *slog.Logger
		wantErr bool
	}{
		{name: "ok", cfg: validConfig(":8080"), log: discard()},
		{name: "nil logger", cfg: validConfig(":8080"), log: nil, wantErr: true},
		{name: "invalid config", cfg: config.Config{LogLevel: "loud", ListenAddr: ":8080"}, log: discard(), wantErr: true},
		{name: "no database url", cfg: noDB, log: discard(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := app.New(tt.cfg, tt.log)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && a == nil {
				t.Fatal("nil app without error")
			}
		})
	}
}

// freeAddr returns a loopback address that was free a moment ago (config rejects port 0).
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

func run(t *testing.T, a *app.App) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- a.Run(t.Context()) }()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestRunFailsWhenTheDatabaseIsUnreachable(t *testing.T) {
	a, err := app.New(validConfig(freeAddr(t)), discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	err = run(t, a)
	if !errors.Is(err, postgres.ErrUnavailable) {
		t.Fatalf("Run = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), "s3cret-marker") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestRunFailsWhenThePortIsBusy(t *testing.T) {
	var lc net.ListenConfig
	busy, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })

	a, err := app.New(validConfig(busy.Addr().String()), discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if err := run(t, a); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("Run = %v, want a listen error", err)
	}
}

func TestRunOnlyOnce(t *testing.T) {
	a, err := app.New(validConfig(freeAddr(t)), discard())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })

	_ = run(t, a) // fails on the database
	if err := run(t, a); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("second Run = %v, want an error", err)
	}
}

func TestRunAfterClose(t *testing.T) {
	a, err := app.New(validConfig(freeAddr(t)), discard())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := a.Run(ctx); err == nil {
		t.Fatal("Run after Close succeeded")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	a, err := app.New(validConfig(":8080"), discard())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := a.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}
