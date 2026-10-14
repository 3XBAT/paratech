package config_test

import (
	"bytes"
	"errors"
	"flag"
	"io"
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
		args    []string
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
		{name: "headless", env: map[string]string{config.EnvHeadless: "true"}, want: config.Config{LogLevel: "info", Headless: true}},
		{name: "not headless", env: map[string]string{config.EnvHeadless: "0"}, want: config.Config{LogLevel: "info"}},
		{name: "bad headless", env: map[string]string{config.EnvHeadless: "maybe"}, wantErr: "is not a boolean"},
		{
			name:    "bad headless with other problems",
			env:     map[string]string{config.EnvHeadless: "maybe", config.EnvLogLevel: "loud"},
			wantErr: "unknown log level",
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
			got, err := config.Load(tt.args, io.Discard, func(k string) string { return tt.env[k] })
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

func TestLoadFlags(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	env := map[string]string{config.EnvLogLevel: "warn"}
	getenv := func(k string) string { return env[k] }

	t.Run("flag overrides environment", func(t *testing.T) {
		got, err := config.Load([]string{"--log-level", "debug", "--data-dir", abs}, io.Discard, getenv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.LogLevel != "debug" || got.DataDir != abs {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("environment is kept without flags", func(t *testing.T) {
		got, err := config.Load(nil, io.Discard, getenv)
		if err != nil || got.LogLevel != "warn" {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})

	t.Run("headless flag overrides environment", func(t *testing.T) {
		got, err := config.Load([]string{"--headless"}, io.Discard, func(k string) string {
			return map[string]string{config.EnvHeadless: "false"}[k]
		})
		if err != nil || !got.Headless {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})

	t.Run("invalid flag value is reported", func(t *testing.T) {
		_, err := config.Load([]string{"--log-level", "loud"}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "unknown log level") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		_, err := config.Load([]string{"--nope"}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("help prints usage", func(t *testing.T) {
		var out bytes.Buffer
		_, err := config.Load([]string{"--help"}, &out, getenv)
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("error = %v, want flag.ErrHelp", err)
		}
		for _, want := range []string{"Usage of", "-log-level", "-data-dir", "-headless"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("usage %q does not contain %q", out.String(), want)
			}
		}
	})
}
