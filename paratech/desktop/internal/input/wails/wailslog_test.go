package wails

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestFloorLogger(t *testing.T) {
	const secret = "s3cret-marker"
	tests := []struct {
		name   string
		logger func(*slog.Logger) *slog.Logger
	}{
		{"plain", func(l *slog.Logger) *slog.Logger { return l }},
		{"with attrs", func(l *slog.Logger) *slog.Logger { return l.With("component", "wails") }},
		{"with group", func(l *slog.Logger) *slog.Logger { return l.WithGroup("wails") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			log := tt.logger(floorLogger(base, wailsLogLevel))

			if log.Enabled(context.Background(), slog.LevelDebug) {
				t.Error("Debug must be disabled under the floor")
			}
			if !log.Enabled(context.Background(), slog.LevelInfo) {
				t.Error("Info must stay enabled")
			}
			log.Debug("Binding call complete:", "args", `["`+secret+`"]`)
			// Handle is guarded too: a caller that skips Enabled must not get past the floor.
			r := slog.NewRecord(time.Time{}, slog.LevelDebug, "direct", 0)
			r.AddAttrs(slog.String("args", secret))
			if err := log.Handler().Handle(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			log.Info("window shown")

			out := buf.String()
			if strings.Contains(out, secret) {
				t.Errorf("debug record with the secret was written: %q", out)
			}
			if !strings.Contains(out, "window shown") {
				t.Errorf("info record missing: %q", out)
			}
		})
	}
}

func TestFloorLoggerKeepsHigherBaseLevel(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	log := floorLogger(base, wailsLogLevel)

	log.Info("quiet")
	log.Warn("loud")
	if out := buf.String(); strings.Contains(out, "quiet") || !strings.Contains(out, "loud") {
		t.Errorf("the user's level must still apply above the floor: %q", out)
	}
}

// The page services of the shell log to Options.Log, and the Wails logger is Options.Log behind the Info floor.
func TestShellLoggers(t *testing.T) {
	const secret = "s3cret-binding-arg"
	var buf bytes.Buffer
	opts := validOptions()
	opts.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.OpenServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Not running: the save dialog fails and the service logs it.
	if got, err := svc.Diagnostics.SaveSupportBundle(context.Background()); err != nil || got.Status != bundleFailed {
		t.Fatalf("SaveSupportBundle = %+v, %v", got, err)
	}
	s.WailsLogger().Debug("Binding call complete:", "args", `["`+secret+`"]`)
	s.WailsLogger().Info("wails info")

	out := buf.String()
	if !strings.Contains(out, opPickBundle) || !strings.Contains(out, "wails info") {
		t.Fatalf("log %q lacks the service or the Wails record", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("the Wails Debug record passed the floor: %q", out)
	}
}
