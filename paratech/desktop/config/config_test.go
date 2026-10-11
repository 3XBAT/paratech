package config_test

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
)

func TestLoad(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr string
	}{
		{name: "defaults", env: nil, want: config.Config{LogLevel: "info"}},
		{
			name: "overrides",
			env:  map[string]string{config.EnvLogLevel: "debug", config.EnvDataDir: abs},
			want: config.Config{LogLevel: "debug", DataDir: abs},
		},
		{name: "bad level", env: map[string]string{config.EnvLogLevel: "loud"}, wantErr: "unknown log level"},
		{name: "relative data dir", env: map[string]string{config.EnvDataDir: "data"}, wantErr: "absolute path"},
		{
			name:    "reports all problems",
			env:     map[string]string{config.EnvLogLevel: "loud", config.EnvDataDir: "data"},
			wantErr: "unknown log level",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"bogus", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := (config.Config{LogLevel: tt.in}).Level(); got != tt.want {
			t.Errorf("Level(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
