package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type countingPurger struct {
	mu    sync.Mutex
	calls int
	err   error
	ran   chan struct{}
}

func (p *countingPurger) PurgeResults(context.Context) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == 1 {
		close(p.ran)
	}
	return 3, p.err
}

func TestPurgeRunsAtStartAndStopsWithTheContext(t *testing.T) {
	for _, err := range []error{nil, errors.New("db down")} {
		p := &countingPurger{err: err, ran: make(chan struct{})}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			purgeResults(ctx, slog.New(slog.DiscardHandler), p, time.Hour)
		}()
		<-p.ran // the first purge runs at once, not after the interval
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the purge loop did not stop with its context")
		}
		if p.calls != 1 {
			t.Fatalf("purge calls = %d, want 1", p.calls)
		}
	}
}
