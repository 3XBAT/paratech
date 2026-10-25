package gateway

import (
	"context"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Provider runs one non-streaming LLM call. Implemented by the anthropic adapter; testenv has a scripted fake.
type Provider interface {
	// Complete sends req and returns the completion with the provider-reported usage. The completion output is
	// UNTRUSTED. A failure is a *values.ProviderError matching values.ErrProviderTransient or
	// values.ErrProviderPermanent; when the caller's ctx ended the call it also matches ctx.Err().
	Complete(ctx context.Context, req values.Request) (values.Completion, error)
}

// StreamingProvider runs one LLM call with the output streamed (ADR-0026). Implemented by the anthropic adapter;
// testenv has a scripted fake.
type StreamingProvider interface {
	// Stream is Provider.Complete with the same request and error rules. onDelta gets the non-empty text chunks in
	// order, from the calling goroutine, before Stream returns; on success their concatenation is the completion
	// output. Chunks are UNTRUSTED, and a failure may follow some of them.
	Stream(ctx context.Context, req values.Request, onDelta func(chunk string)) (values.Completion, error)
}

// Accounts stores quota accounts. It has the shape of the quota use case's port (ARCH-20 forbids importing it,
// ADR-0008); the postgres adapter and the account aggregate serve both.
type Accounts interface {
	// Modify runs fn on the account of scope.Owner, loaded as account.Scope describes, inside one transaction that
	// serializes every command of that owner. If fn returns nil, the account's Changes are stored atomically; if it
	// returns an error, nothing is stored and the error is returned unchanged.
	Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error
}

// Results clears stored results whose result window has ended (PRD section 13.2). Implemented by the postgres adapter.
type Results interface {
	// PurgeResults clears the results of key records created at or before cutoff and returns how many it cleared.
	PurgeResults(ctx context.Context, cutoff time.Time) (int64, error)
}

// RateLimiter decides whether owner may make one more call at now; when not, it says how long to wait. Implemented by
// the in-memory ratelimit adapter over requestlimits.Rate.
type RateLimiter interface {
	Allow(owner shared.APIKeyID, now time.Time) (retryAfter time.Duration, ok bool)
}

// Clock supplies the time of a command (ARCH-24).
type Clock interface {
	Now() time.Time
}

// Random fills p with cryptographically secure random bytes for reservation ids (ARCH-24).
type Random interface {
	Fill(p []byte) error
}
