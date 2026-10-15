//go:build !windows || !(amd64 || arm64)

package wasapi

import (
	"context"
	"log/slog"
)

// Options configures the opener.
type Options struct {
	Log *slog.Logger
}

// NewOpener returns an opener that always fails with ErrUnsupported: WASAPI exists only on 64-bit Windows here.
func NewOpener(Options) Opener {
	return func(context.Context, Spec, Sink) (Stream, error) { return nil, ErrUnsupported }
}
