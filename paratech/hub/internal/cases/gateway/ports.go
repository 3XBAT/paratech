package gateway

import (
	"context"

	"paratech/hub/internal/domain/llm/values"
)

// Provider runs one non-streaming LLM call. Implemented by the anthropic adapter; testenv has a scripted fake.
type Provider interface {
	// Complete sends req and returns the completion with the provider-reported usage. The completion output is
	// UNTRUSTED. A failure is a *values.ProviderError matching values.ErrProviderTransient or
	// values.ErrProviderPermanent; when the caller's ctx ended the call it also matches ctx.Err().
	Complete(ctx context.Context, req values.Request) (values.Completion, error)
}
