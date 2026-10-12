package testenv

import (
	"context"
	"errors"
	"testing"
	"time"
)

var clockT0 = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func awaitCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestFakeClockAdvance(t *testing.T) {
	c := NewFakeClock(clockT0)
	c.Advance(-time.Hour) // ignored
	c.Advance(0)
	if !c.Now().Equal(clockT0) {
		t.Fatalf("Now = %v after no-op advances", c.Now())
	}
	c.Advance(90 * time.Second)
	if want := clockT0.Add(90 * time.Second); !c.Now().Equal(want) {
		t.Fatalf("Now = %v, want %v", c.Now(), want)
	}
}

func TestFakeClockSleepWakesWhenDue(t *testing.T) {
	c := NewFakeClock(clockT0)
	ctx := awaitCtx(t)
	done := make(chan error, 2)
	for _, d := range []time.Duration{time.Minute, time.Hour} {
		go func() { done <- c.Sleep(ctx, d) }()
	}
	if err := c.AwaitSleepers(ctx, 2); err != nil {
		t.Fatal(err)
	}

	c.Advance(time.Minute)
	if err := <-done; err != nil {
		t.Fatalf("first Sleep = %v", err)
	}
	if n := c.Sleepers(); n != 1 {
		t.Fatalf("Sleepers = %d, want 1 (the hour sleeper)", n)
	}

	c.Advance(time.Hour)
	if err := <-done; err != nil {
		t.Fatalf("second Sleep = %v", err)
	}
}

func TestFakeClockSleepHonoursContext(t *testing.T) {
	c := NewFakeClock(clockT0)
	ctx, cancel := context.WithCancel(awaitCtx(t))
	done := make(chan error, 1)
	go func() { done <- c.Sleep(ctx, time.Hour) }()
	if err := c.AwaitSleepers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v, want context.Canceled", err)
	}
	if n := c.Sleepers(); n != 0 {
		t.Fatalf("Sleepers = %d after cancel", n)
	}
}

func TestFakeClockSleepEdgeCases(t *testing.T) {
	c := NewFakeClock(clockT0)
	if err := c.Sleep(context.Background(), 0); err != nil {
		t.Fatalf("Sleep(0) = %v", err)
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ended, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep with an ended context = %v", err)
	}
	if err := c.AwaitSleepers(ended, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("AwaitSleepers with an ended context = %v", err)
	}
}
