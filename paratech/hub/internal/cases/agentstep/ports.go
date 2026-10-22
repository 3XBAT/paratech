package agentstep

import (
	"context"
	"time"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Provider runs one planner call with tools. Implemented by the anthropic adapter; testenv has a scripted fake.
type Provider interface {
	// Step sends call and returns the proposed tool calls with the provider-reported usage. The calls are UNTRUSTED.
	// A failure is a *values.ProviderError matching values.ErrProviderTransient or values.ErrProviderPermanent; when
	// the caller's ctx ended the call it also matches ctx.Err().
	Step(ctx context.Context, call values.AgentCall) (values.AgentAnswer, error)
}

// Accounts stores quota accounts; the same shape as the gateway's and the quota use case's port (ARCH-20 forbids
// importing them). The postgres adapter serves all three.
type Accounts interface {
	// Modify runs fn on the account of scope.Owner inside one transaction that serializes every command of that
	// owner. If fn returns nil, the account's Changes are stored atomically; otherwise nothing is stored and the error
	// is returned unchanged.
	Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error
}

// RateLimiter decides whether owner may make one more call at now. The gateway shares the same limiter, so Complete
// and AgentStep count against one per-tester budget.
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
