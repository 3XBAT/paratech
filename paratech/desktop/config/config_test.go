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
		{
			name: "data dir below local app data",
			env:  map[string]string{config.EnvLocalAppData: abs},
			want: config.Config{LogLevel: "info", DataDir: filepath.Join(abs, "LearningAssistant")},
		},
		{
			name: "explicit data dir wins over local app data",
			env:  map[string]string{config.EnvLocalAppData: filepath.Join(abs, "local"), config.EnvDataDir: abs},
			want: config.Config{LogLevel: "info", DataDir: abs},
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

	t.Run("recorder child mode", func(t *testing.T) {
		got, err := config.Load([]string{"--recorder", "--data-dir", abs}, io.Discard, getenv)
		if err != nil || !got.Recorder || got.DataDir != abs {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})

	t.Run("recorder without a data dir", func(t *testing.T) {
		_, err := config.Load([]string{"--recorder"}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "needs a data dir") {
			t.Fatalf("error = %v", err)
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
		for _, want := range []string{"Usage of", "-log-level", "-data-dir", "-headless", "-recorder"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("usage %q does not contain %q", out.String(), want)
			}
		}
	})
}

// A launch by a paratech: link (ADR-0013) parses no flag at all: the link and whatever follows it are ignored, so the
// app starts with the environment and the defaults.
func TestLoadIgnoresASchemeLaunch(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	evil := filepath.Join(t.TempDir(), "evil")
	getenv := func(k string) string { return map[string]string{config.EnvDataDir: abs}[k] }
	tests := []struct {
		name string
		args []string
	}{
		{name: "open", args: []string{"paratech://open"}},
		{name: "upper case", args: []string{"PARATECH://open"}},
		{name: "path and query", args: []string{`paratech://open/../x?--data-dir=C:\evil`}},
		{name: "flag in the link", args: []string{"paratech:--headless"}},
		{name: "flags after a link split at a quote", args: []string{"paratech://x", "--data-dir", evil, "--headless"}},
		{name: "flags before the link", args: []string{"--data-dir", evil, "paratech://open"}},
		{name: "help after the link", args: []string{"paratech://open", "--help"}},
		{name: "unknown flag after the link", args: []string{"paratech:", "--nope"}},
		{name: "recorder", args: []string{"paratech://open", "--recorder"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(tt.args, io.Discard, getenv)
			if err != nil {
				t.Fatalf("Load(%q) = %v", tt.args, err)
			}
			if want := (config.Config{LogLevel: "info", DataDir: abs}); got != want {
				t.Fatalf("Load(%q) = %+v, want %+v", tt.args, got, want)
			}
		})
	}
}

// Arguments that only look like the scheme are still parsed.
func TestLoadParsesOtherArguments(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "data")
	for _, args := range [][]string{{"--headless", "paratec"}, {"--headless", "x-paratech://open"}} {
		got, err := config.Load(args, io.Discard, func(string) string { return "" })
		if err != nil || !got.Headless {
			t.Fatalf("Load(%q) = %+v, %v", args, got, err)
		}
	}
	got, err := config.Load([]string{"--data-dir", abs, "--headless"}, io.Discard, func(string) string { return "" })
	if err != nil || got.DataDir != abs || !got.Headless {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}
