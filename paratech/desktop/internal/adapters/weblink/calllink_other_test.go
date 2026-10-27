//go:build !windows

package weblink

import (
	"context"
	"errors"
	"testing"
)

func TestOpenCallLinkIsUnsupported(t *testing.T) {
	if err := New().OpenCallLink(context.Background(), mustCallLink(t, zoomCallLink)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenCallLink = %v, want ErrUnsupported", err)
	}
}
