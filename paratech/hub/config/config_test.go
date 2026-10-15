package config_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"paratech/hub/config"
)

// devURL is a syntactically valid URL; the password is a marker that must never show up in output.
const devURL = "postgres://hub:s3cret-marker@127.0.0.1:55432/hub?sslmode=disable"

func withDB(env map[string]string) map[string]string {
	out := map[string]string{config.EnvDatabaseURL: devURL}
	for k, v := range env {
		out[k] = v
	}
	return out
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    config.Config
		wantErr string
	}{
		{
			name: "defaults",
			env:  withDB(nil),
			want: config.Config{LogLevel: "info", ListenAddr: ":8080", DatabaseURL: devURL, ShutdownTimeout: 15 * time.Second},
		},
		{
			name: "overrides",
			env: withDB(map[string]string{
				config.EnvLogLevel: "debug", config.EnvListenAddr: "127.0.0.1:9000", config.EnvShutdownTimeout: "3s",
			}),
			want: config.Config{LogLevel: "debug", ListenAddr: "127.0.0.1:9000", DatabaseURL: devURL, ShutdownTimeout: 3 * time.Second},
		},
		{name: "bad level", env: withDB(map[string]string{config.EnvLogLevel: "loud"}), wantErr: "unknown log level"},
		{name: "no port", env: withDB(map[string]string{config.EnvListenAddr: "localhost"}), wantErr: "listen address"},
		{name: "port out of range", env: withDB(map[string]string{config.EnvListenAddr: ":70000"}), wantErr: "port must be"},
		{name: "port not a number", env: withDB(map[string]string{config.EnvListenAddr: ":http"}), wantErr: "port must be"},
		{name: "database url missing", env: nil, wantErr: config.EnvDatabaseURL + " is required"},
		{name: "database url bad scheme", env: map[string]string{config.EnvDatabaseURL: "mysql://u:s3cret-marker@h/db"}, wantErr: "scheme must be"},
		{name: "database url keyword form", env: map[string]string{config.EnvDatabaseURL: "host=h password=s3cret-marker dbname=db"}, wantErr: "scheme must be"},
		{name: "database url unparsable", env: map[string]string{config.EnvDatabaseURL: "postgres://u:s3cret-marker@h:port/db"}, wantErr: "not a valid URL"},
		{name: "database url no host", env: map[string]string{config.EnvDatabaseURL: "postgres:///db"}, wantErr: "host is required"},
		{name: "database url no database", env: map[string]string{config.EnvDatabaseURL: "postgres://u:s3cret-marker@h:5432/"}, wantErr: "database name is required"},
		{name: "shutdown timeout not a duration", env: withDB(map[string]string{config.EnvShutdownTimeout: "soon"}), wantErr: "not a duration"},
		{name: "shutdown timeout zero", env: withDB(map[string]string{config.EnvShutdownTimeout: "0s"}), wantErr: "shutdown timeout"},
		{name: "shutdown timeout too long", env: withDB(map[string]string{config.EnvShutdownTimeout: "1h"}), wantErr: "shutdown timeout"},
		{
			name:    "reports all problems",
			env:     map[string]string{config.EnvLogLevel: "loud", config.EnvListenAddr: "localhost"},
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
				if strings.Contains(err.Error(), "s3cret-marker") {
					t.Fatalf("error leaks the password: %v", err)
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

func TestLoadReportsAllProblems(t *testing.T) {
	_, err := config.Load(nil, io.Discard, func(k string) string {
		return map[string]string{config.EnvLogLevel: "loud", config.EnvShutdownTimeout: "soon"}[k]
	})
	for _, want := range []string{"unknown log level", "not a duration", config.EnvDatabaseURL + " is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want containing %q", err, want)
		}
	}
}

func TestConfigDoesNotPrintTheDatabaseURL(t *testing.T) {
	cfg, err := config.Load(nil, io.Discard, func(k string) string { return withDB(nil)[k] })
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	slog.New(slog.NewTextHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	for _, out := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%v", &cfg), logged.String()} {
		if strings.Contains(out, "s3cret-marker") || strings.Contains(out, "127.0.0.1:55432") {
			t.Errorf("output leaks the database URL: %s", out)
		}
		if !strings.Contains(out, "redacted") {
			t.Errorf("output %s does not mark the database URL as redacted", out)
		}
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
	env := withDB(map[string]string{config.EnvLogLevel: "warn"})
	getenv := func(k string) string { return env[k] }

	t.Run("flag overrides environment", func(t *testing.T) {
		got, err := config.Load([]string{"--log-level", "debug", "--listen", ":9090", "--shutdown-timeout", "2s"}, io.Discard, getenv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.LogLevel != "debug" || got.ListenAddr != ":9090" || got.ShutdownTimeout != 2*time.Second {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("environment is kept without flags", func(t *testing.T) {
		got, err := config.Load(nil, io.Discard, getenv)
		if err != nil || got.LogLevel != "warn" {
			t.Fatalf("got %+v, err %v", got, err)
		}
	})

	t.Run("invalid flag value is reported", func(t *testing.T) {
		_, err := config.Load([]string{"--log-level", "loud"}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "unknown log level") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("database url is not a flag", func(t *testing.T) {
		_, err := config.Load([]string{"--database-url", devURL}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
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
		for _, want := range []string{"Usage of", "-log-level", "-listen", "-shutdown-timeout", config.EnvDatabaseURL} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("usage %q does not contain %q", out.String(), want)
			}
		}
		if strings.Contains(out.String(), "s3cret-marker") {
			t.Errorf("usage leaks the password: %s", out.String())
		}
	})
}
