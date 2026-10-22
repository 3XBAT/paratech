package agentstep

import (
	"errors"
	"fmt"
	"time"

	"paratech/hub/internal/domain/quota/account"
)

// Errors of the scenario. Their texts never carry request text, model output or provider messages.
var (
	// ErrInvalidConfig means New got a missing dependency or an invalid configuration.
	ErrInvalidConfig = errors.New("agentstep: invalid configuration")
	// ErrInvalidRequest means the request breaks a limit or a format rule of ADR-0023. The wrapped domain error says
	// which rule.
	ErrInvalidRequest = errors.New("invalid agent step request")
	// ErrUnknownCatalog means the profile has no catalog of the requested version. It matches ErrInvalidRequest.
	ErrUnknownCatalog = fmt.Errorf("%w: unknown catalog version", ErrInvalidRequest)
	// ErrBadSeal means an echo of calls does not carry the Hub's seal for this caller and thread. It matches
	// ErrInvalidRequest.
	ErrBadSeal = fmt.Errorf("%w: the seal of echoed calls does not match", ErrInvalidRequest)
	// ErrIdempotencyConflict means the key was used with a different request (in any state of its attempts).
	ErrIdempotencyConflict = errors.New("idempotency key used with a different request")
	// ErrAttemptRunning means an attempt with the same key is still running; retry with backoff.
	ErrAttemptRunning = errors.New("an attempt with this idempotency key is still running")
	// ErrTurnGone means the Hub no longer keeps (or never kept) the model's turn of the last echoed calls, so the tool
	// loop cannot go on; nothing was charged. Permanent: the thread starts again from the student's text.
	ErrTurnGone = errors.New("the model turn of the echoed calls is no longer kept")
)

// RateLimitedError means the tester exceeded the rate limit; nothing was reserved.
type RateLimitedError struct {
	RetryAfter time.Duration
}

// Error implements error.
func (e *RateLimitedError) Error() string {
	return "rate limited, retry after " + e.RetryAfter.String()
}

// QuotaExceededError means the remaining quota is below the most the step may cost; nothing was reserved.
// errors.Is(err, account.ErrQuotaExceeded) matches it.
type QuotaExceededError struct {
	Quota Snapshot
}

// Error implements error.
func (e *QuotaExceededError) Error() string { return account.ErrQuotaExceeded.Error() }

// Unwrap lets errors.Is see account.ErrQuotaExceeded.
func (e *QuotaExceededError) Unwrap() error { return account.ErrQuotaExceeded }

// ProviderFailedError means the provider call failed, or its answer was not a valid set of tool calls. A failed call
// or a refusal released the reservation and nothing was charged; any other answer was charged its reported usage and
// a retry with the same key returns this error again. Transient says whether repeating the same request may help.
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
