package ratelimit

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func owner(n int) shared.APIKeyID { return shared.APIKeyID(fmt.Sprintf("%016x", n)) }

func limiter(t *testing.T, perMinute, burst int) *Limiter {
	t.Helper()
	rate, err := requestlimits.NewRate(perMinute, burst)
	if err != nil {
		t.Fatal(err)
	}
	l, err := New(rate)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewRejectsAZeroRate(t *testing.T) {
	if _, err := New(requestlimits.Rate{}); !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("err = %v, want ErrInvalidRate", err)
	}
}

func TestAllowIsPerTester(t *testing.T) {
	l := limiter(t, 60, 2)
	for range 2 {
		if _, ok := l.Allow(owner(1), t0); !ok {
			t.Fatal("burst refused")
		}
	}
	if wait, ok := l.Allow(owner(1), t0); ok || wait != time.Second {
		t.Fatalf("third call: ok %t wait %s", ok, wait)
	}
	if _, ok := l.Allow(owner(2), t0); !ok {
		t.Fatal("another tester is limited by the first one")
	}
	if _, ok := l.Allow(owner(1), t0.Add(time.Second)); !ok {
		t.Fatal("refused after the wait")
	}
}

func TestConcurrentCallsNeverExceedTheBurst(t *testing.T) {
	l := limiter(t, 1, 5)
	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := l.Allow(owner(1), t0); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 5 {
		t.Fatalf("%d calls allowed, want the burst of 5", allowed)
	}
}

func TestIdleStatesAreDropped(t *testing.T) {
	l := limiter(t, 60, 1)
	for i := range minSweep - 1 {
		l.Allow(owner(i), t0)
	}
	if l.Len() != minSweep-1 {
		t.Fatalf("Len = %d", l.Len())
	}
	// Much later every old state is idle: the sweep at the threshold drops them all.
	l.Allow(owner(minSweep), t0.Add(time.Hour))
	if l.Len() != 1 {
		t.Fatalf("Len after the sweep = %d, want 1", l.Len())
	}
}
