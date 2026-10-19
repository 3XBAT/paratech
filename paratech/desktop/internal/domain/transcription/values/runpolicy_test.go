package values

import (
	"errors"
	"testing"
)

func TestNewRunPolicy(t *testing.T) {
	tests := []struct {
		threads, cap int
		ok           bool
	}{
		{1, 0, true},
		{MaxRunThreads, 100, true},
		{4, 40, true},
		{0, 0, false},
		{MaxRunThreads + 1, 0, false},
		{4, -1, false},
		{4, 101, false},
	}
	for _, tt := range tests {
		p, err := NewRunPolicy(tt.threads, true, tt.cap)
		if tt.ok {
			if err != nil || p.Threads() != tt.threads || !p.LowPriority() || p.CPUCapPercent() != tt.cap {
				t.Errorf("NewRunPolicy(%d, %d) = %+v, %v", tt.threads, tt.cap, p, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidRunPolicy) {
			t.Errorf("NewRunPolicy(%d, %d) err = %v, want ErrInvalidRunPolicy", tt.threads, tt.cap, err)
		}
	}
}
