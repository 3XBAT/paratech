package connect

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDeltaSenderIsTheOnlyWriter: deltas pushed from many goroutines and heartbeats on a 1 ms interval never overlap in
// Send, and nothing is sent once finish has returned.
func TestDeltaSenderIsTheOnlyWriter(t *testing.T) {
	var inFlight, sends, beats atomic.Int64
	enter := func() {
		if inFlight.Add(1) != 1 {
			t.Error("two sends at once")
		}
		time.Sleep(100 * time.Microsecond) // widen the window a concurrent send would hit
		inFlight.Add(-1)
	}
	d := newDeltaSender(func(string) error {
		enter()
		sends.Add(1)
		return nil
	}, heartbeat{every: time.Millisecond, send: func() error {
		enter()
		beats.Add(1)
		return nil
	}})

	burst := func() {
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				for range 50 {
					d.push("x")
					time.Sleep(50 * time.Microsecond)
				}
			})
		}
		wg.Wait()
	}
	burst()
	time.Sleep(20 * time.Millisecond) // a silence: heartbeats go out between the bursts
	burst()
	if err := d.finish(false); err != nil {
		t.Fatal(err)
	}

	after := sends.Load() + beats.Load()
	time.Sleep(10 * time.Millisecond)
	if sends.Load()+beats.Load() != after {
		t.Fatal("a send after finish")
	}
	if sends.Load() == 0 || beats.Load() == 0 {
		t.Fatalf("sends %d, heartbeats %d; want both", sends.Load(), beats.Load())
	}
}

// TestDeltaSenderStopsOnAHeartbeatError: a failed heartbeat (the client is gone) stops the sender; finish reports it.
func TestDeltaSenderStopsOnAHeartbeatError(t *testing.T) {
	gone := errors.New("client gone")
	var beats atomic.Int64
	d := newDeltaSender(func(string) error {
		t.Error("a delta after the failed heartbeat")
		return nil
	}, heartbeat{every: time.Millisecond, send: func() error {
		beats.Add(1)
		return gone
	}})
	for beats.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	d.push("late")
	if err := d.finish(false); !errors.Is(err, gone) {
		t.Fatalf("finish = %v, want the heartbeat error", err)
	}
	if beats.Load() != 1 {
		t.Fatalf("heartbeats = %d, want 1", beats.Load())
	}
}
