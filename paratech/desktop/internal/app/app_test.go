package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"paratech/desktop/config"
	"paratech/desktop/internal/app"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// headless is the default configuration without the UI shell: tests never open windows.
func headless() config.Config {
	cfg := config.Default()
	cfg.Headless = true
	return cfg
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		log     *slog.Logger
		wantErr bool
	}{
		{name: "ok", cfg: config.Default(), log: discard()},
		{name: "headless", cfg: headless(), log: discard()},
		{name: "nil logger", cfg: config.Default(), log: nil, wantErr: true},
		{name: "invalid config", cfg: config.Config{LogLevel: "loud"}, log: discard(), wantErr: true},
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

func TestRunBlocksUntilCancelled(t *testing.T) {
	a, err := app.New(headless(), discard())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	select {
	case <-done:
		t.Fatal("Run returned before the context was cancelled")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	a, err := app.New(config.Default(), discard())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := a.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}
}
