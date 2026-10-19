package values

import (
	"testing"
	"time"
)

func TestAssembledTrackSilent(t *testing.T) {
	tests := []struct {
		name             string
		seconds, audible int
		silent           bool
	}{
		{"all zeros", 600, 0, true},
		{"two audible seconds", 10, 2, true},
		{"three audible seconds of a short track", 10, 3, false},
		{"below one percent", 3600, 35, true},
		{"one percent", 3600, 36, false},
		{"empty", 0, 0, true},
	}
	for _, tt := range tests {
		tr := AssembledTrack{Duration: time.Duration(tt.seconds) * time.Second, Seconds: tt.seconds, AudibleSeconds: tt.audible}
		if got := tr.Silent(); got != tt.silent {
			t.Errorf("%s: Silent = %v, want %v", tt.name, got, tt.silent)
		}
	}
}

func TestHeardSince(t *testing.T) {
	tests := []struct {
		name      string
		prev, cur CaptureStats
		heard     bool
	}{
		{"first look with sound", CaptureStats{}, CaptureStats{Packets: 10, AudiblePackets: 2}, true},
		{"first look silent", CaptureStats{}, CaptureStats{Packets: 10, SilentPackets: 10}, false},
		{"audible packets grew", CaptureStats{Packets: 10, AudiblePackets: 2}, CaptureStats{Packets: 20, AudiblePackets: 3}, true},
		{"no new audible packets", CaptureStats{Packets: 10, AudiblePackets: 2}, CaptureStats{Packets: 20, AudiblePackets: 2}, false},
		{"new child with sound", CaptureStats{Packets: 100, AudiblePackets: 50}, CaptureStats{Packets: 5, AudiblePackets: 1}, true},
		{"new child silent", CaptureStats{Packets: 100, AudiblePackets: 50}, CaptureStats{Packets: 5}, false},
	}
	for _, tt := range tests {
		if got := tt.cur.HeardSince(tt.prev); got != tt.heard {
			t.Errorf("%s: HeardSince = %v, want %v", tt.name, got, tt.heard)
		}
	}
}
