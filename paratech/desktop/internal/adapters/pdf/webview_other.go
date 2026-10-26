//go:build !windows

package pdf

import (
	"context"
	"errors"
	"log/slog"
)

// ErrUnsupported means this computer cannot print to PDF; outside Windows that is always the case.
// The same values are declared in webview_windows.go.
var ErrUnsupported = errors.New("pdf: webview2 runtime 98.0.1108.43 or newer is required")

// ErrFailed means the runtime is present but this print did not produce a PDF.
var ErrFailed = errors.New("pdf: printing failed")

// Printer is the stub for other operating systems: PDF printing needs WebView2.
type Printer struct{}

// NewPrinter returns the stub; its arguments are accepted for wiring parity with Windows.
func NewPrinter(_ string, _ *slog.Logger) *Printer { return &Printer{} }

// Print always fails with ErrUnsupported.
func (p *Printer) Print(_ context.Context, _ []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
