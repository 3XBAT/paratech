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
