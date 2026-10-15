package config_test

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"paratech/hub/config"
)

func TestLoadRateLimit(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		env           map[string]string
		perMin, burst int
		wantErr       string
	}{
		{name: "defaults", perMin: 30, burst: 10},
		{name: "environment", env: map[string]string{config.EnvRateLimitPerMinute: "120", config.EnvRateLimitBurst: "1"}, perMin: 120, burst: 1},
		{name: "upper bounds", env: map[string]string{config.EnvRateLimitPerMinute: "6000", config.EnvRateLimitBurst: "1000"}, perMin: 6000, burst: 1000},
		{
			name: "flags win", args: []string{"--rate-limit-per-minute", "5", "--rate-limit-burst", "2"},
			env: map[string]string{config.EnvRateLimitPerMinute: "7", config.EnvRateLimitBurst: "3"}, perMin: 5, burst: 2,
		},
		{name: "per minute not a number", env: map[string]string{config.EnvRateLimitPerMinute: "fast"}, wantErr: config.EnvRateLimitPerMinute},
		{name: "burst not a number", env: map[string]string{config.EnvRateLimitBurst: "1.5"}, wantErr: config.EnvRateLimitBurst},
		{name: "per minute zero", env: map[string]string{config.EnvRateLimitPerMinute: "0"}, wantErr: "rate limit per minute"},
		{name: "per minute too high", args: []string{"--rate-limit-per-minute", "6001"}, wantErr: "rate limit per minute"},
		{name: "burst zero", env: map[string]string{config.EnvRateLimitBurst: "0"}, wantErr: "rate limit burst"},
		{name: "burst too high", env: map[string]string{config.EnvRateLimitBurst: "1001"}, wantErr: "rate limit burst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := withDB(tt.env)
			got, err := config.Load(tt.args, io.Discard, func(k string) string { return env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got.RateLimitPerMinute != tt.perMin || got.RateLimitBurst != tt.burst {
				t.Fatalf("got %d/%d, %v; want %d/%d", got.RateLimitPerMinute, got.RateLimitBurst, err, tt.perMin, tt.burst)
			}
		})
	}
}

func TestRateLimitIsCheckedOnlyForServe(t *testing.T) {
	env := withDB(map[string]string{config.EnvRateLimitPerMinute: "0"})
	if _, err := config.Load([]string{"keys", "list"}, io.Discard, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("keys list with a bad rate limit: %v", err)
	}
}

func TestConfigPrintsRateLimit(t *testing.T) {
	cfg := config.Default()
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	if !strings.Contains(cfg.String(), "RateLimitPerMinute:30 RateLimitBurst:10") || !strings.Contains(logged.String(), `"rate_limit_burst":10`) {
		t.Fatalf("output lacks the rate limit: %s %s", cfg.String(), logged.String())
	}
}
