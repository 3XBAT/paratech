package backoff

import (
	"testing"
	"time"
)

func TestInteractive(t *testing.T) {
	tests := []struct {
		attempt int
		wait    time.Duration
		ok      bool
	}{
		{attempt: -1},
		{attempt: 0},
		{attempt: 1, wait: 5 * time.Second, ok: true},
		{attempt: 2, wait: 10 * time.Second, ok: true},
		{attempt: 3, wait: 20 * time.Second, ok: true},
		{attempt: 4},
		{attempt: 100},
	}
	for _, tt := range tests {
		wait, ok := Interactive(tt.attempt)
		if wait != tt.wait || ok != tt.ok {
			t.Errorf("Interactive(%d) = %v, %v; want %v, %v", tt.attempt, wait, ok, tt.wait, tt.ok)
		}
	}
}
