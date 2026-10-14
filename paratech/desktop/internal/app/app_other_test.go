//go:build !windows

package app_test

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/config"
	"paratech/desktop/internal/app"
	"paratech/desktop/internal/input/wails"
)

func TestRunWithShellIsUnsupportedOffWindows(t *testing.T) {
	a, err := app.New(config.Default(), discard())
	if err != nil {
		t.Fatal(err)
	}

	err = a.Run(context.Background())
	if !errors.Is(err, wails.ErrUnsupported) {
		t.Fatalf("Run error = %v, want ErrUnsupported", err)
	}
}
