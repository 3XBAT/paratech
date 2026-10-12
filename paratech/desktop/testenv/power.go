package testenv

import (
	"context"
	"sync"
)

// FakePower is a PowerMonitor with manual switching.
type FakePower struct {
	mu   sync.Mutex
	onAC bool
	subs map[chan bool]struct{}
}

// NewFakePower returns a fake that starts on the given power source.
func NewFakePower(onAC bool) *FakePower {
	return &FakePower{onAC: onAC, subs: map[chan bool]struct{}{}}
}

// Set switches the power source and notifies subscribers if it changed.
func (f *FakePower) Set(onAC bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onAC == onAC {
		return
	}
	f.onAC = onAC
	for ch := range f.subs {
		publish(ch, onAC)
	}
}

// IsOnACPower reports the current source.
func (f *FakePower) IsOnACPower(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onAC, nil
}

// Subscribe sends the current state first, then every change; the channel is closed when ctx is done.
func (f *FakePower) Subscribe(ctx context.Context) (<-chan bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan bool, 1)
	f.mu.Lock()
	ch <- f.onAC
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		f.mu.Lock()
		delete(f.subs, ch)
		close(ch) // under the lock: Set never sends on a closed channel
		f.mu.Unlock()
	}()
	return ch, nil
}

// publish replaces an unread value; callers hold the lock, so the final send cannot block.
func publish(ch chan bool, v bool) {
	select {
	case ch <- v:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- v
	}
}
