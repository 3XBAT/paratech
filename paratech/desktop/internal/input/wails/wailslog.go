package wails

import (
	"context"
	"log/slog"
)

// wailsLogLevel is the lowest level Wails may log at: at Debug it writes every binding call with its JSON arguments,
// which include the mail password passed to SetMailPassword.
const wailsLogLevel = slog.LevelInfo

// levelFloor drops records below min whatever the wrapped handler accepts.
type levelFloor struct {
	min  slog.Level
	next slog.Handler
}

// floorLogger returns a logger writing to log's handler that never emits records below min.
func floorLogger(log *slog.Logger, min slog.Level) *slog.Logger {
	return slog.New(levelFloor{min: min, next: log.Handler()})
}

func (h levelFloor) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.next.Enabled(ctx, l)
}

func (h levelFloor) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min {
		return nil // a caller may skip Enabled
	}
	return h.next.Handle(ctx, r)
}

func (h levelFloor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return levelFloor{min: h.min, next: h.next.WithAttrs(attrs)}
}

func (h levelFloor) WithGroup(name string) slog.Handler {
	return levelFloor{min: h.min, next: h.next.WithGroup(name)}
}
