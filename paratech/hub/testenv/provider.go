package testenv

import (
	"context"
	"sync"

	"paratech/hub/internal/domain/llm/values"
)

// ProviderStep is one scripted answer of FakeProvider.
type ProviderStep struct {
	// Completion is returned when Err is nil and Block is false.
	Completion values.Completion
	// Err is returned instead of a completion.
	Err error
	// Block makes the call wait until its context ends and return a transient timeout error matching ctx.Err(), the
	// way the real adapter reports a call cut short.
	Block bool
	// Entered, when not nil, is closed when the call starts this step (to synchronize with a blocked call).
	Entered chan struct{}
}

// FakeProvider is a scripted provider for the gateway's Provider port. Each call takes the next step; a call without
// a step left fails with a permanent error. Calls are recorded. Safe for concurrent use.
type FakeProvider struct {
	mu    sync.Mutex
	steps []ProviderStep
	calls []values.Request
}

// NewFakeProvider returns a provider that answers with steps in order.
func NewFakeProvider(steps ...ProviderStep) *FakeProvider {
	return &FakeProvider{steps: steps}
}

// Push appends steps to the script.
func (p *FakeProvider) Push(steps ...ProviderStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, steps...)
}

// Complete records req and plays the next step. A context that has already ended fails the call like the real
// adapter does, without consuming a step.
func (p *FakeProvider) Complete(ctx context.Context, req values.Request) (values.Completion, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req)
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return values.Completion{}, cutShort(err)
	}
	if len(p.steps) == 0 {
		p.mu.Unlock()
		return values.Completion{}, &values.ProviderError{Kind: values.KindPermanent, Reason: "fake provider: no scripted step"}
	}
	step := p.steps[0]
	p.steps = p.steps[1:]
	p.mu.Unlock()

	if step.Entered != nil {
		close(step.Entered)
	}
	switch {
	case step.Block:
		<-ctx.Done()
		return values.Completion{}, cutShort(ctx.Err())
	case step.Err != nil:
		return values.Completion{}, step.Err
	default:
		return step.Completion, nil
	}
}

// Calls returns a copy of the recorded requests, oldest first.
func (p *FakeProvider) Calls() []values.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]values.Request(nil), p.calls...)
}

// Remaining returns the number of steps not played yet.
func (p *FakeProvider) Remaining() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.steps)
}

func cutShort(cause error) error {
	return &values.ProviderError{Kind: values.KindTransient, Reason: "timeout", Cause: cause}
}
