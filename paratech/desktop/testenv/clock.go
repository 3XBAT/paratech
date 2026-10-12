package testenv

import (
	"context"
	"sync"
	"time"
)

// FakeClock is a manually advanced clock that also serves as a sleeper: Sleep blocks until Advance moves the time
// far enough or the context ends. It satisfies the Clock and Sleeper ports of the use cases.
type FakeClock struct {
	mu       sync.Mutex
	now      time.Time
	sleepers map[*fakeSleeper]struct{}
	changed  chan struct{} // closed and replaced whenever the set of sleepers changes
}

type fakeSleeper struct {
	at   time.Time
	wake chan struct{}
}

// NewFakeClock returns a clock that shows start until it is advanced.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start, sleepers: map[*fakeSleeper]struct{}{}, changed: make(chan struct{})}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the time forward and wakes every sleeper that is due. A negative step is ignored.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d <= 0 {
		return
	}
	c.now = c.now.Add(d)
	woke := false
	for s := range c.sleepers {
		if !s.at.After(c.now) {
			close(s.wake)
			delete(c.sleepers, s)
			woke = true
		}
	}
	if woke {
		c.notify()
	}
}

// Sleep blocks until d has passed on the fake clock (nil) or ctx ends (its error). A non-positive d returns at once.
func (c *FakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	c.mu.Lock()
	s := &fakeSleeper{at: c.now.Add(d), wake: make(chan struct{})}
	c.sleepers[s] = struct{}{}
	c.notify()
	c.mu.Unlock()

	select {
	case <-s.wake:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		if _, still := c.sleepers[s]; still {
			delete(c.sleepers, s)
			c.notify()
		}
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Sleepers returns how many goroutines are blocked in Sleep.
func (c *FakeClock) Sleepers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sleepers)
}

// AwaitSleepers blocks until at least n goroutines sleep, so a test advances the clock only once the code under test
// is idle. It returns ctx's error if ctx ends first.
func (c *FakeClock) AwaitSleepers(ctx context.Context, n int) error {
	for {
		c.mu.Lock()
		ready, changed := len(c.sleepers) >= n, c.changed
		c.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// notify wakes AwaitSleepers callers; c.mu must be held.
func (c *FakeClock) notify() {
	close(c.changed)
	c.changed = make(chan struct{})
}
