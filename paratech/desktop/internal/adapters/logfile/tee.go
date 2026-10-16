package logfile

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
)

// Tee is a slog handler that passes every record to the handler it wraps and, once Attach gave it a writer, also
// writes it as a text line there. It decides nothing itself: Enabled is the wrapped handler's, so the file gets what
// the console gets, and a level floor in front of a Tee (the Wails logger) holds for the file too. Handlers derived
// with WithAttrs or WithGroup share the attachment, so loggers built before Attach write to the file as well. A
// failed write to the file is dropped: the wrapped handler still has the record.
type Tee struct {
	next  slog.Handler
	sink  *sink
	ops   []func(slog.Handler) slog.Handler // the WithAttrs and WithGroup calls since the root, replayed on the file
	cache *atomic.Pointer[derived]
}

// sink holds the text handler of the attached writer; nil while none is attached.
type sink struct {
	base atomic.Pointer[slog.TextHandler]
}

// derived is the file handler of a derived Tee for one base.
type derived struct {
	base *slog.TextHandler
	h    slog.Handler
}

// NewTee wraps next.
func NewTee(next slog.Handler) *Tee {
	return &Tee{next: next, sink: &sink{}, cache: &atomic.Pointer[derived]{}}
}

// Attach makes every Tee of this root also write to w (at every level the wrapped handler enables).
func (t *Tee) Attach(w io.Writer) {
	t.sink.base.Store(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Detach stops the writes to the attached writer; call it before closing that writer.
func (t *Tee) Detach() { t.sink.base.Store(nil) }

// Enabled is the wrapped handler's answer.
func (t *Tee) Enabled(ctx context.Context, l slog.Level) bool { return t.next.Enabled(ctx, l) }

// Handle passes r on and writes it to the attached writer.
func (t *Tee) Handle(ctx context.Context, r slog.Record) error {
	err := t.next.Handle(ctx, r.Clone())
	if h := t.file(); h != nil {
		_ = h.Handle(ctx, r)
	}
	return err
}

// WithAttrs returns a Tee whose records carry attrs on both sides.
func (t *Tee) WithAttrs(attrs []slog.Attr) slog.Handler {
	return t.derive(t.next.WithAttrs(attrs), func(h slog.Handler) slog.Handler { return h.WithAttrs(attrs) })
}

// WithGroup returns a Tee whose attributes go into group name on both sides.
func (t *Tee) WithGroup(name string) slog.Handler {
	return t.derive(t.next.WithGroup(name), func(h slog.Handler) slog.Handler { return h.WithGroup(name) })
}

func (t *Tee) derive(next slog.Handler, op func(slog.Handler) slog.Handler) *Tee {
	ops := append(append([]func(slog.Handler) slog.Handler(nil), t.ops...), op)
	return &Tee{next: next, sink: t.sink, ops: ops, cache: &atomic.Pointer[derived]{}}
}

// file is the handler of the attached writer with this Tee's attributes and groups, nil when none is attached.
func (t *Tee) file() slog.Handler {
	base := t.sink.base.Load()
	if base == nil {
		return nil
	}
	if d := t.cache.Load(); d != nil && d.base == base {
		return d.h
	}
	var h slog.Handler = base
	for _, op := range t.ops {
		h = op(h)
	}
	t.cache.Store(&derived{base: base, h: h})
	return h
}
