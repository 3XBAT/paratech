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

func TestLoadCommands(t *testing.T) {
	// The key commands do not listen: a bad listen address or shutdown timeout in the environment does not stop them.
	env := withDB(map[string]string{config.EnvListenAddr: "localhost", config.EnvShutdownTimeout: "1h"})
	getenv := func(k string) string { return env[k] }

	tests := []struct {
		name    string
		args    []string
		want    config.Command
		label   string
		id      string
		wantErr string
	}{
		{name: "create", args: []string{"keys", "create", "-label", "tester-07"}, want: config.CommandKeysCreate, label: "tester-07"},
		{name: "create after global flags", args: []string{"--log-level", "debug", "keys", "create", "--label=t1"}, want: config.CommandKeysCreate, label: "t1"},
		{name: "revoke", args: []string{"keys", "revoke", "-id", "0123456789abcdef"}, want: config.CommandKeysRevoke, id: "0123456789abcdef"},
		{name: "list", args: []string{"keys", "list"}, want: config.CommandKeysList},
		{name: "create without label", args: []string{"keys", "create"}, wantErr: "-label is required"},
		{name: "revoke without id", args: []string{"keys", "revoke"}, wantErr: "-id is required"},
		{name: "keys without subcommand", args: []string{"keys"}, wantErr: "want a subcommand"},
		{name: "unknown subcommand", args: []string{"keys", "delete"}, wantErr: "unknown subcommand"},
		{name: "unknown command", args: []string{"serve-all"}, wantErr: "unknown command"},
		{name: "extra argument", args: []string{"keys", "list", "now"}, wantErr: "unexpected argument"},
		{name: "flag of another subcommand", args: []string{"keys", "list", "-label", "x"}, wantErr: "flag provided but not defined"},
		{name: "global flag after the command", args: []string{"keys", "list", "--log-level", "debug"}, wantErr: "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(tt.args, io.Discard, getenv)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Command != tt.want || got.KeyLabel != tt.label || got.KeyID != tt.id {
				t.Fatalf("got %+v (label %q, id %q)", got, got.KeyLabel, got.KeyID)
			}
		})
	}

	t.Run("serve still checks the listen address", func(t *testing.T) {
		if _, err := config.Load(nil, io.Discard, getenv); err == nil || !strings.Contains(err.Error(), "listen address") {
			t.Fatalf("error = %v, want a listen address error", err)
		}
	})

	t.Run("key commands need the database URL", func(t *testing.T) {
		_, err := config.Load([]string{"keys", "list"}, io.Discard, func(string) string { return "" })
		if err == nil || !strings.Contains(err.Error(), config.EnvDatabaseURL+" is required") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("unknown command value", func(t *testing.T) {
		cfg := config.Default()
		cfg.DatabaseURL = devURL
		cfg.Command = config.Command(99)
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestCommandHelp(t *testing.T) {
	getenv := func(k string) string { return withDB(nil)[k] }
	for _, args := range [][]string{{"keys", "--help"}, {"keys", "create", "-h"}, {"keys", "revoke", "--help"}} {
		var out bytes.Buffer
		_, err := config.Load(args, &out, getenv)
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("%v: error = %v, want flag.ErrHelp", args, err)
		}
		if !strings.Contains(out.String(), "Usage of hub keys") {
			t.Errorf("%v: usage %q", args, out.String())
		}
	}

	var out bytes.Buffer
	_, _ = config.Load([]string{"keys", "create", "-h"}, &out, getenv)
	if !strings.Contains(out.String(), "-label") {
		t.Errorf("create usage does not describe -label: %q", out.String())
	}
}

func TestCommandString(t *testing.T) {
	for c, want := range map[config.Command]string{
		config.CommandServe: "serve", config.CommandKeysCreate: "keys create", config.CommandKeysRevoke: "keys revoke",
		config.CommandKeysList: "keys list", config.Command(99): "Command(99)",
	} {
		if got := c.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", int(c), got, want)
		}
	}
}
