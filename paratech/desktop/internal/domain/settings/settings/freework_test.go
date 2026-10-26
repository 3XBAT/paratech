package settings

import (
	"testing"
	"time"
)

func TestFreeWorkNotice(t *testing.T) {
	if !New().FreeWorkNotice() {
		t.Fatal("a fresh installation shows the free-work card")
	}
	for _, stored := range []bool{true, false} {
		s, err := Restore(State{Version: 1, FreeWorkNotice: stored})
		if err != nil || s.FreeWorkNotice() != stored {
			t.Fatalf("Restore(%v) = %v, %v", stored, s, err)
		}
	}

	s := New()
	if err := s.SetFreeWorkNotice(true, t1); err != nil || !s.UpdatedAt().IsZero() {
		t.Fatalf("same value: err %v, updated %v; want a no-op", err, s.UpdatedAt())
	}
	if err := s.SetFreeWorkNotice(false, t1); err != nil || s.FreeWorkNotice() || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("off: err %v, on %v at %v", err, s.FreeWorkNotice(), s.UpdatedAt())
	}
	if err := s.SetFreeWorkNotice(true, time.Time{}); err == nil || s.FreeWorkNotice() {
		t.Fatalf("a change without a time = %v", err)
	}
	if err := s.SetFreeWorkNotice(true, t0); err != nil || !s.FreeWorkNotice() {
		t.Fatalf("on again: %v", err)
	}
}
