package testenv

import (
	"sync"
	"time"

	"paratech/hub/internal/domain/shared"
)

// FakeLimiter is a fake of the gateway's RateLimiter port: it allows every call until Refuse is called, then refuses
// with that wait. Calls are counted. Safe for concurrent use.
type FakeLimiter struct {
	mu    sync.Mutex
	wait  time.Duration
	calls int
}

// Refuse makes every following call fail with retryAfter; 0 allows calls again.
func (l *FakeLimiter) Refuse(retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wait = retryAfter
}

// Allow implements the port.
func (l *FakeLimiter) Allow(shared.APIKeyID, time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.wait > 0 {
		return l.wait, false
	}
	return 0, true
}

// Calls returns the number of Allow calls.
func (l *FakeLimiter) Calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}
