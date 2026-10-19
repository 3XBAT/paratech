package loadpolicy

import (
	"testing"

	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

func TestFor(t *testing.T) {
	tests := []struct {
		load    shared.RecognitionLoad
		cpus    int
		threads int
		low     bool
		cap     int
	}{
		{shared.LoadGentle, 16, 4, true, 40},
		{shared.LoadGentle, 8, 4, true, 40},
		{shared.LoadGentle, 6, 3, true, 40},
		{shared.LoadGentle, 3, 1, true, 40},
		{shared.LoadGentle, 1, 1, true, 40},
		{shared.LoadGentle, 0, 1, true, 40},
		{shared.LoadNormal, 8, 4, true, 0},
		{shared.LoadNormal, 2, 2, true, 0},
		{shared.LoadNormal, -3, 1, true, 0},
		{shared.LoadMax, 8, 8, false, 0},
		{shared.LoadMax, 1, 1, false, 0},
		{shared.LoadMax, 1000, values.MaxRunThreads, false, 0},
		{"", 8, 4, true, 40},
		{"turbo", 8, 4, true, 40},
	}
	for _, tt := range tests {
		p := New().For(tt.load, tt.cpus)
		if p.Threads() != tt.threads || p.LowPriority() != tt.low || p.CPUCapPercent() != tt.cap {
			t.Errorf("For(%q, %d) = threads %d, low %v, cap %d; want %d, %v, %d", tt.load, tt.cpus,
				p.Threads(), p.LowPriority(), p.CPUCapPercent(), tt.threads, tt.low, tt.cap)
		}
	}
}
