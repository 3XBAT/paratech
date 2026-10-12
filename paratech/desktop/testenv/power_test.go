package testenv

import (
	"context"
	"testing"
	"time"
)

type powerMonitor interface {
	IsOnACPower(ctx context.Context) (bool, error)
	Subscribe(ctx context.Context) (<-chan bool, error)
}

var _ powerMonitor = (*FakePower)(nil)

func recv(t *testing.T, ch <-chan bool) bool {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a value")
	}
	return false
}

func TestFakePowerIsOnACPower(t *testing.T) {
	for _, start := range []bool{true, false} {
		f := NewFakePower(start)
		got, err := f.IsOnACPower(context.Background())
		if err != nil || got != start {
			t.Fatalf("start %v: got %v, %v", start, got, err)
		}
		f.Set(!start)
		if got, _ := f.IsOnACPower(context.Background()); got != !start {
			t.Fatalf("after Set(%v) got %v", !start, got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewFakePower(true).IsOnACPower(ctx); err == nil {
		t.Fatal("want error for cancelled ctx")
	}
}

func TestFakePowerBroadcastsToAllSubscribers(t *testing.T) {
	f := NewFakePower(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := f.Subscribe(ctx)
	b, _ := f.Subscribe(ctx)
	for _, ch := range []<-chan bool{a, b} {
		if !recv(t, ch) {
			t.Fatal("first value must be the current state (true)")
		}
	}
	f.Set(false)
	f.Set(false) // no change, no event
	f.Set(true)
	for name, ch := range map[string]<-chan bool{"a": a, "b": b} {
		// a reader that did not keep up sees only the latest state
		if !recv(t, ch) {
			t.Fatalf("subscriber %s: want latest state true", name)
		}
	}
}

func TestFakePowerSlowReaderNeverBlocksSender(t *testing.T) {
	f := NewFakePower(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := f.Subscribe(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for i := range 100 {
			f.Set(i%2 == 0)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Set blocked on a slow subscriber")
	}
}

func TestFakePowerCancelClosesChannelAndUnsubscribes(t *testing.T) {
	f := NewFakePower(false)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := f.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recv(t, ch) {
		t.Fatal("want false")
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("want closed channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
	f.Set(true) // must not panic on a closed subscriber
	f.mu.Lock()
	n := len(f.subs)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("subscribers left: %d", n)
	}
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if _, err := f.Subscribe(cctx); err == nil {
		t.Fatal("want error for cancelled ctx")
	}
}
