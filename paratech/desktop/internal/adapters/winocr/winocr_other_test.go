//go:build !windows

package winocr

import (
	"context"
	"errors"
	"testing"
)

func TestStubIsUnsupported(t *testing.T) {
	r := New()
	if _, err := r.Read(context.Background(), []byte("\x89PNG\r\n\x1a\n")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read = %v, want ErrUnsupported", err)
	}
	if _, err := r.AvailableLanguages(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("AvailableLanguages = %v, want ErrUnsupported", err)
	}
}
