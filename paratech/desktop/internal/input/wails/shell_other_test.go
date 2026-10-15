//go:build !windows

package wails

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/notice"
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

// Without a UI shell there are no notifications; the notifier reports it like the window commands.
func TestNotifyWithoutShell(t *testing.T) {
	s, err := New(validOptions())
	if err != nil {
		t.Fatal(err)
	}
	n, err := notice.New("job-1", "stt", job.StatusCompleted, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run: err = %v, want ErrUnsupported", err)
	}
	if err := s.Notify(context.Background(), n); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Notify: err = %v, want ErrNotRunning", err)
	}
	if s.Ready() {
		t.Fatal("Ready without a shell")
	}
}
