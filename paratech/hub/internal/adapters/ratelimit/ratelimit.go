// Package ratelimit keeps the per-tester state of the gateway's rate policy (requestlimits.Rate) in process memory.
// It implements the RateLimiter port of the gateway use case. The state is lost on restart and not shared between
// Hub processes; the policy itself lives in the domain.
package ratelimit

import (
	"errors"
	"sync"
	"time"

	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/shared"
)

// minSweep is the number of tracked testers at which idle states are first dropped.
const minSweep = 1024

// ErrInvalidRate means New got a rate that was not built by requestlimits.NewRate.
var ErrInvalidRate = errors.New("ratelimit: invalid rate")

// Limiter is safe for concurrent use. Only authenticated testers reach it, so it holds at most one state per issued
// API key; idle states (equal to no state) are dropped as the map grows.
type Limiter struct {
	rate requestlimits.Rate

	mu        sync.Mutex
	tats      map[shared.APIKeyID]time.Time
	nextSweep int
}

// New returns a limiter for rate.
func New(rate requestlimits.Rate) (*Limiter, error) {
	if !rate.Valid() {
		return nil, ErrInvalidRate
	}
	return &Limiter{rate: rate, tats: map[shared.APIKeyID]time.Time{}, nextSweep: minSweep}, nil
}

// Allow takes one call of owner at now. When the call is refused it returns how long to wait.
func (l *Limiter) Allow(owner shared.APIKeyID, now time.Time) (retryAfter time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	next, wait, ok := l.rate.Take(l.tats[owner], now)
	if !ok {
		return wait, false
	}
	l.tats[owner] = next
	if len(l.tats) >= l.nextSweep {
		l.sweep(now)
	}
	return 0, true
}

// Len returns the number of tracked testers.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.tats)
}

// sweep drops idle states; the next sweep waits until the map doubles, so busy maps are not scanned on every call.
func (l *Limiter) sweep(now time.Time) {
	for owner, tat := range l.tats {
		if l.rate.Idle(tat, now) {
			delete(l.tats, owner)
		}
	}
	l.nextSweep = max(minSweep, 2*len(l.tats))
}
