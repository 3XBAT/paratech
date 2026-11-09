package connect

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDeltaSenderIsTheOnlyWriter: deltas pushed from many goroutines and heartbeats fired by a test-driven ticker never
// overlap in Send, and nothing is sent once finish has returned. No wall-clock timing decides the outcome.
func TestDeltaSenderIsTheOnlyWriter(t *testing.T) {
	var inFlight, sends, beats atomic.Int64
	ticks := make(chan time.Time) // unbuffered: a completed send means the loop took the tick
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
	}, heartbeat{ticks: ticks, send: func() error {
		enter()
		beats.Add(1)
		return nil
	}})

	// Ticks arrive while the bursts run, so heartbeats compete with the deltas for the writer.
	stop := make(chan struct{})
	var ticker sync.WaitGroup
	ticker.Go(func() {
		for {
			select {
			case ticks <- time.Time{}:
			case <-stop:
				return
			}
		}
	})

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
	burst()
	close(stop)
	ticker.Wait()

	// The loop is idle again once it takes a tick, so the tick before it has been answered: two ticks, one beat at least.
	ticks <- time.Time{}
	ticks <- time.Time{}
	if err := d.finish(false); err != nil {
		t.Fatal(err)
	}

	after := sends.Load() + beats.Load()
	runtime.Gosched()
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
	ticks := make(chan time.Time)
	d := newDeltaSender(func(string) error {
		t.Error("a delta after the failed heartbeat")
		return nil
	}, heartbeat{ticks: ticks, send: func() error {
		beats.Add(1)
		return gone
	}})
	ticks <- time.Time{}
	for beats.Load() == 0 {
		runtime.Gosched()
	}
	d.push("late")
	if err := d.finish(false); !errors.Is(err, gone) {
		t.Fatalf("finish = %v, want the heartbeat error", err)
	}
	if beats.Load() != 1 {
		t.Fatalf("heartbeats = %d, want 1", beats.Load())
	}
}
