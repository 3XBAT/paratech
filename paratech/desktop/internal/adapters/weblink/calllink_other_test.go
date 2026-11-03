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
	if err := New().OpenSubjectCallLink(context.Background(), mustSubjectCallLink(t, zoomCallLink)); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OpenSubjectCallLink = %v, want ErrUnsupported", err)
	}
}
