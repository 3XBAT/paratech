package lesson

import (
	"testing"
	"time"
)

// The lesson of newLesson runs from t0 to t0+90m.
func TestInProgress(t *testing.T) {
	tests := []struct {
		name    string
		now     time.Time
		deleted bool
		want    bool
	}{
		{"before the start", t0.Add(-time.Nanosecond), false, false},
		{"at the start", t0, false, true},
		{"in the middle", t0.Add(45 * time.Minute), false, true},
		{"just before the end", t0.Add(90*time.Minute - time.Nanosecond), false, true},
		{"at the end", t0.Add(90 * time.Minute), false, false},
		{"another zone, same instant", t0.Add(time.Minute).In(time.FixedZone("UTC+3", 3*3600)), false, true},
		{"no time", time.Time{}, false, false},
		{"deleted", t0.Add(time.Minute), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLesson(t)
			if tt.deleted {
				if err := l.MarkDeleted(t0); err != nil {
					t.Fatal(err)
				}
			}
			if got := l.InProgress(tt.now); got != tt.want {
				t.Fatalf("InProgress(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}
