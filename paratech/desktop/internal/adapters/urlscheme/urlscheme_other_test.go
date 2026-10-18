//go:build !windows

package urlscheme

import (
	"errors"
	"testing"
)

func TestEnsureUnsupportedOffWindows(t *testing.T) {
	if _, err := New().Ensure(`C:\paratech.exe`); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Ensure = %v, want errors.ErrUnsupported", err)
	}
}
