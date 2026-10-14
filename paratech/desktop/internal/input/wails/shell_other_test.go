//go:build !windows

package wails

import (
	"context"
	"errors"
	"testing"
)

func TestRunIsUnsupported(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Run(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run: err = %v, want ErrUnsupported", err)
	}
	if err := s.ShowPopup(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("ShowPopup after Run: err = %v, want ErrNotRunning", err)
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Run: err = %v, want ErrAlreadyStarted", err)
	}
}
