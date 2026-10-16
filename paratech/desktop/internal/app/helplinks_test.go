package app

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/adapters/weblink"
	"paratech/desktop/internal/domain/transcription/values"
	"paratech/desktop/internal/input/wails"
)

// TestHelpPagesPassTheLinkCheck runs every page of the help screens (TASK-052) through the production opener's check.
// The context is cancelled first, so a page that passes reaches the launcher and stops before anything starts:
// exec.Cmd.Start returns the context's error on Windows, other systems have no launcher.
func TestHelpPagesPassTheLinkCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for key, raw := range wails.HelpPageURLs() {
		t.Run(key, func(t *testing.T) {
			u, err := values.ParseSourceURL(raw)
			if err != nil {
				t.Fatalf("ParseSourceURL(%q): %v", raw, err)
			}
			err = weblink.New().OpenLink(ctx, u)
			if errors.Is(err, weblink.ErrRefused) {
				t.Fatalf("weblink refuses %q: %v", raw, err)
			}
			if !errors.Is(err, context.Canceled) && !errors.Is(err, weblink.ErrUnsupported) {
				t.Fatalf("OpenLink(%q) = %v, want the cancelled start or ErrUnsupported", raw, err)
			}
		})
	}
}
