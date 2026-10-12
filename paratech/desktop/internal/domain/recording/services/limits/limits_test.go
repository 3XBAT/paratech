package limits

import (
	"errors"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name string
		cap  time.Duration
		ok   bool
	}{
		{"default", 3 * time.Hour, true},
		{"lower bound", time.Minute, true},
		{"upper bound", 12 * time.Hour, true},
		{"zero", 0, false},
		{"negative", -time.Hour, false},
		{"below lower bound", time.Minute - time.Nanosecond, false},
		{"above upper bound", 12*time.Hour + time.Nanosecond, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(tc.cap)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidPolicy) {
					t.Fatalf("error = %v, want ErrInvalidPolicy", err)
				}
				return
			}
			if err != nil || p.MaxDuration() != tc.cap {
				t.Fatalf("New(%v) = %v, %v", tc.cap, p.MaxDuration(), err)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	if got := Default().MaxDuration(); got != 3*time.Hour {
		t.Fatalf("default cap = %v, want 3h (PRD section 6.2)", got)
	}
}

func TestAllowStart(t *testing.T) {
	cases := []struct {
		inProgress int
		ok         bool
	}{{0, true}, {1, false}, {2, false}}
	for _, tc := range cases {
		err := Default().AllowStart(tc.inProgress)
		if tc.ok != (err == nil) {
			t.Fatalf("AllowStart(%d) = %v", tc.inProgress, err)
		}
		if err != nil && !errors.Is(err, ErrRecordingInProgress) {
			t.Fatalf("error = %v, want ErrRecordingInProgress", err)
		}
	}
}
