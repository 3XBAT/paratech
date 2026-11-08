package settings

import (
	"errors"
	"testing"
	"time"
)

// TASK-276: the student is asked on a fresh installation and on an older row; only known modes are taken.
func TestCalendarOnDueChangeDefaultIsAsk(t *testing.T) {
	if DefaultCalendarOnDueChange != CalendarAsk {
		t.Fatalf("default = %q, want ask (ADR-0029 З3)", DefaultCalendarOnDueChange)
	}
	if got := New().CalendarOnDueChange(); got != CalendarAsk {
		t.Fatalf("fresh installation: %q, want ask", got)
	}
}

func TestCalendarOnDueChangeRestore(t *testing.T) {
	tests := []struct {
		stored CalendarOnDueChange
		want   CalendarOnDueChange
	}{
		{stored: "", want: CalendarAsk}, // a state from before the setting
		{stored: CalendarAsk, want: CalendarAsk},
		{stored: CalendarUpdate, want: CalendarUpdate},
		{stored: CalendarKeep, want: CalendarKeep},
		{stored: "always", want: CalendarAsk}, // unknown: never change the event without the student's say
		{stored: "UPDATE", want: CalendarAsk},
	}
	for _, tt := range tests {
		s, err := Restore(State{Version: 1, CalendarOnDueChange: tt.stored})
		if err != nil || s.CalendarOnDueChange() != tt.want {
			t.Errorf("Restore(%q) = %v, %v; want %q", tt.stored, s, err, tt.want)
		}
	}
}

func TestSetCalendarOnDueChange(t *testing.T) {
	tests := []struct {
		name    string
		mode    CalendarOnDueChange
		now     time.Time
		want    CalendarOnDueChange
		updated time.Time
		err     bool
	}{
		{name: "same value is a no-op", mode: CalendarAsk, now: t1, want: CalendarAsk},
		{name: "update", mode: CalendarUpdate, now: t1, want: CalendarUpdate, updated: t1},
		{name: "keep", mode: CalendarKeep, now: t1, want: CalendarKeep, updated: t1},
		{name: "unknown", mode: "always", now: t1, want: CalendarAsk, err: true},
		{name: "empty", mode: "", now: t1, want: CalendarAsk, err: true},
		{name: "no change time", mode: CalendarUpdate, want: CalendarAsk, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			err := s.SetCalendarOnDueChange(tt.mode, tt.now)
			if tt.err != errors.Is(err, ErrInvalidSettings) || (!tt.err && err != nil) {
				t.Fatalf("err = %v, want error %v", err, tt.err)
			}
			if s.CalendarOnDueChange() != tt.want || !s.UpdatedAt().Equal(tt.updated) {
				t.Fatalf("mode %q updated %v; want %q at %v", s.CalendarOnDueChange(), s.UpdatedAt(), tt.want, tt.updated)
			}
		})
	}

	s := New()
	if err := s.SetCalendarOnDueChange(CalendarKeep, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCalendarOnDueChange(CalendarAsk, t1); err != nil || s.CalendarOnDueChange() != CalendarAsk {
		t.Fatalf("ask again: %q, %v", s.CalendarOnDueChange(), err)
	}
}
