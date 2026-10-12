package backoff

import (
	"errors"
	"math"
	"testing"
	"time"
)

func mustPolicy(t *testing.T, s Spec) Policy {
	t.Helper()
	p, err := New(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewRejectsInvalidSpecs(t *testing.T) {
	ok := Spec{Base: time.Second, Factor: 2, MaxDelay: time.Minute, MaxAttempts: 3, Jitter: 0.5}
	tests := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"zero base", func(s *Spec) { s.Base = 0 }},
		{"negative base", func(s *Spec) { s.Base = -time.Second }},
		{"factor below one", func(s *Spec) { s.Factor = 0.5 }},
		{"NaN factor", func(s *Spec) { s.Factor = math.NaN() }},
		{"infinite factor", func(s *Spec) { s.Factor = math.Inf(1) }},
		{"max delay below base", func(s *Spec) { s.MaxDelay = time.Millisecond }},
		{"no attempts", func(s *Spec) { s.MaxAttempts = 0 }},
		{"negative jitter", func(s *Spec) { s.Jitter = -0.1 }},
		{"jitter above one", func(s *Spec) { s.Jitter = 1.1 }},
		{"NaN jitter", func(s *Spec) { s.Jitter = math.NaN() }},
	}
	if _, err := New(ok); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if _, err := New(DefaultSpec()); err != nil {
		t.Fatalf("default spec rejected: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := ok
			tc.mutate(&s)
			if _, err := New(s); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("New = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestDelay(t *testing.T) {
	plain := Spec{Base: time.Second, Factor: 2, MaxDelay: 10 * time.Second, MaxAttempts: 9}
	jittered := plain
	jittered.Jitter = 0.5
	flat := Spec{Base: time.Second, Factor: 1, MaxDelay: time.Second, MaxAttempts: 9}

	tests := []struct {
		name     string
		spec     Spec
		failures int
		unit     float64
		want     time.Duration
	}{
		{"first failure", plain, 1, 0, time.Second},
		{"second doubles", plain, 2, 0, 2 * time.Second},
		{"third", plain, 3, 0, 4 * time.Second},
		{"fourth", plain, 4, 0, 8 * time.Second},
		{"fifth capped", plain, 5, 0, 10 * time.Second},
		{"huge count stays capped", plain, 1 << 30, 0, 10 * time.Second},
		{"zero failures counts as first", plain, 0, 0, time.Second},
		{"negative failures counts as first", plain, -3, 0, time.Second},
		{"constant factor", flat, 7, 0, time.Second},
		{"jitter unit zero keeps the delay", jittered, 2, 0, 2 * time.Second},
		{"jitter unit half", jittered, 2, 0.5, 1500 * time.Millisecond},
		{"jitter unit clamped above", jittered, 2, 7, time.Second},
		{"jitter unit clamped below", jittered, 2, -1, 2 * time.Second},
		{"jitter unit NaN", jittered, 2, math.NaN(), 2 * time.Second},
		{"jitter never exceeds the cap", jittered, 9, 0, 10 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustPolicy(t, tc.spec).Delay(tc.failures, tc.unit); got != tc.want {
				t.Fatalf("Delay(%d, %v) = %v, want %v", tc.failures, tc.unit, got, tc.want)
			}
		})
	}
}

func TestDelayIsMonotonicAndBounded(t *testing.T) {
	p := mustPolicy(t, DefaultSpec())
	prev := time.Duration(0)
	for n := 1; n <= 50; n++ {
		d := p.Delay(n, 0)
		if d < prev || d > p.Spec().MaxDelay {
			t.Fatalf("Delay(%d) = %v after %v, cap %v", n, d, prev, p.Spec().MaxDelay)
		}
		prev = d
	}
}

func TestNextStopsAtMaxAttempts(t *testing.T) {
	p := mustPolicy(t, Spec{Base: time.Second, Factor: 2, MaxDelay: time.Minute, MaxAttempts: 3})
	tests := []struct {
		attempts  int
		wantDelay time.Duration
		wantRetry bool
	}{
		{1, time.Second, true},
		{2, 2 * time.Second, true},
		{3, 0, false},
		{4, 0, false},
	}
	for _, tc := range tests {
		delay, retry := p.Next(tc.attempts, 0)
		if delay != tc.wantDelay || retry != tc.wantRetry {
			t.Errorf("Next(%d) = %v, %v; want %v, %v", tc.attempts, delay, retry, tc.wantDelay, tc.wantRetry)
		}
	}
}
