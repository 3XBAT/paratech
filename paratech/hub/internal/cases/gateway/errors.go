package gateway

import (
	"errors"
	"time"

	"paratech/hub/internal/domain/quota/account"
)

// Errors of the scenario. Their texts never carry request text, model output or provider messages.
var (
	// ErrInvalidConfig means New got a missing dependency or an invalid configuration.
	ErrInvalidConfig = errors.New("gateway: invalid configuration")

	// ErrInvalidRequest means the request breaks a format rule or names a model alias that is not allowed. The
	// wrapped domain error says which rule.
	ErrInvalidRequest = errors.New("invalid completion request")

	// ErrIdempotencyConflict means the key was used with a different request (in any state of its attempts).
	ErrIdempotencyConflict = errors.New("idempotency key used with a different request")

	// ErrAttemptRunning means an attempt with the same key is still running; retry with backoff.
	ErrAttemptRunning = errors.New("an attempt with this idempotency key is still running")
)

// RateLimitedError means the tester exceeded the rate limit; nothing was reserved.
type RateLimitedError struct {
	RetryAfter time.Duration
}

// Error implements error.
func (e *RateLimitedError) Error() string {
	return "rate limited, retry after " + e.RetryAfter.String()
}

// QuotaExceededError means the remaining quota is below the most the call may cost; nothing was reserved. Quota is
// the tester's quota at that moment. errors.Is(err, account.ErrQuotaExceeded) matches it.
type QuotaExceededError struct {
	Quota Snapshot
}

// Error implements error.
func (e *QuotaExceededError) Error() string { return account.ErrQuotaExceeded.Error() }

// Unwrap lets errors.Is see account.ErrQuotaExceeded.
func (e *QuotaExceededError) Unwrap() error { return account.ErrQuotaExceeded }

// ProviderFailedError means the provider call failed (or refused to answer); the reservation was released and nothing
// was charged. Transient says whether repeating the same request may help; RetryAfter is the provider's wait, 0 when
// none. The cause is a *values.ProviderError, which carries only fixed texts.
type ProviderFailedError struct {
	Transient  bool
	RetryAfter time.Duration
	Cause      error
}

// Error implements error.
func (e *ProviderFailedError) Error() string {
	if e.Cause == nil {
		return "provider failed"
	}
	return "provider failed: " + e.Cause.Error()
}

// Unwrap returns the cause.
func (e *ProviderFailedError) Unwrap() error { return e.Cause }
