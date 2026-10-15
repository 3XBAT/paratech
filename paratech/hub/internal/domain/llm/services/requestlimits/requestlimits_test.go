package requestlimits

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func spec(t *testing.T, instr, data string) values.Spec {
	t.Helper()
	s, err := values.NewSpec("k", "summary", "default", instr, data, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReservationIsAnUpperBound(t *testing.T) {
	s := spec(t, "Summarize.", "урок") // 10 + 8 bytes
	if got := MaxInputTokens(s); got != 18+FramingTokens {
		t.Fatalf("MaxInputTokens = %d", got)
	}
	got, err := Reservation(s, 512)
	if err != nil || got != 18+FramingTokens+512 {
		t.Fatalf("Reservation = %d, %v", got, err)
	}

	big := spec(t, strings.Repeat("i", values.MaxPayloadBytes/2), strings.Repeat("d", values.MaxPayloadBytes/2))
	if got, err := Reservation(big, 8192); err != nil || got != values.MaxPayloadBytes+FramingTokens+8192 {
		t.Fatalf("Reservation of 1 MiB = %d, %v", got, err)
	}
}

func TestReservationRejectsBadCaps(t *testing.T) {
	s := spec(t, "i", "")
	for _, maxOutput := range []int64{0, -1, math.MaxInt64} {
		if _, err := Reservation(s, maxOutput); !errors.Is(err, ErrInvalidMaxOutput) {
			t.Errorf("Reservation(%d) error = %v, want ErrInvalidMaxOutput", maxOutput, err)
		}
	}
}

func TestNewRate(t *testing.T) {
	r, err := NewRate(30, 10)
	if err != nil || !r.Valid() || r.interval != 2*time.Second || r.window != 20*time.Second {
		t.Fatalf("NewRate(30, 10) = %+v, %v", r, err)
	}
	for _, c := range [][2]int{{0, 1}, {-1, 1}, {MaxPerMinute + 1, 1}, {1, 0}, {1, MaxBurst + 1}} {
		if _, err := NewRate(c[0], c[1]); !errors.Is(err, ErrInvalidRate) {
			t.Errorf("NewRate(%d, %d) error = %v, want ErrInvalidRate", c[0], c[1], err)
		}
	}
	if (Rate{}).Valid() {
		t.Fatal("the zero Rate must not be valid")
	}
}

func TestRateBurstThenSteady(t *testing.T) {
	r, err := NewRate(60, 3) // one call a second, three at once
	if err != nil {
		t.Fatal(err)
	}
	var tat time.Time
	for i := range 3 {
		next, _, ok := r.Take(tat, t0)
		if !ok {
			t.Fatalf("call %d of the burst refused", i+1)
		}
		tat = next
	}
	next, wait, ok := r.Take(tat, t0)
	if ok || wait != time.Second || !next.Equal(tat) {
		t.Fatalf("4th call: ok %t, wait %s, state moved %t; want refused for 1s", ok, wait, !next.Equal(tat))
	}

	// Half the wait is not enough; the full wait is.
	if _, wait, ok := r.Take(tat, t0.Add(500*time.Millisecond)); ok || wait != 500*time.Millisecond {
		t.Fatalf("after 0.5s: ok %t, wait %s", ok, wait)
	}
	if tat, _, ok = r.Take(tat, t0.Add(time.Second)); !ok {
		t.Fatal("after the wait the call is refused")
	}

	// A long idle period refills the burst, but not beyond it.
	later := t0.Add(time.Hour)
	if !r.Idle(tat, later) {
		t.Fatal("state after an hour is not idle")
	}
	allowed := 0
	for range 10 {
		if next, _, ok := r.Take(tat, later); ok {
			tat, allowed = next, allowed+1
		}
	}
	if allowed != 3 {
		t.Fatalf("after idling %d calls allowed, want the burst of 3", allowed)
	}
	if r.Idle(tat, later) {
		t.Fatal("a used state is idle")
	}
}

func TestRateSurvivesAClockStepBack(t *testing.T) {
	r, err := NewRate(60, 2)
	if err != nil {
		t.Fatal(err)
	}
	tat, _, _ := r.Take(time.Time{}, t0)
	tat, _, _ = r.Take(tat, t0)
	// The clock goes back an hour: the tester is not locked out for an hour.
	if _, _, ok := r.Take(tat, t0.Add(-time.Hour)); !ok {
		t.Fatal("a clock step back locked the tester out")
	}
}
