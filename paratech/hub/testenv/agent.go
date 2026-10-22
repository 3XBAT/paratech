package testenv

import (
	"context"
	"sync"

	"paratech/hub/internal/domain/llm/values"
)

// AgentProviderStep is one scripted answer of FakeAgentProvider.
type AgentProviderStep struct {
	// Answer is returned when Err is nil.
	Answer values.AgentAnswer
	// Err is returned instead of an answer.
	Err error
	// Entered, when not nil, is closed when the call starts this step.
	Entered chan struct{}
	// Wait, when not nil, holds the call until Wait is closed or its context ends (then it fails like the adapter).
	Wait chan struct{}
}

// FakeAgentProvider is a scripted provider for the agent step's Provider port. Each call takes the next step; a call
// without a step left fails with a permanent error. Calls are recorded. Safe for concurrent use.
type FakeAgentProvider struct {
	mu    sync.Mutex
	steps []AgentProviderStep
	calls []values.AgentCall
}

// NewFakeAgentProvider returns a provider that answers with steps in order.
func NewFakeAgentProvider(steps ...AgentProviderStep) *FakeAgentProvider {
	return &FakeAgentProvider{steps: steps}
}

// Push appends steps to the script.
func (p *FakeAgentProvider) Push(steps ...AgentProviderStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, steps...)
}

// Step records call and plays the next step. A context that has already ended fails the call without consuming a
// step.
func (p *FakeAgentProvider) Step(ctx context.Context, call values.AgentCall) (values.AgentAnswer, error) {
	p.mu.Lock()
	p.calls = append(p.calls, call)
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return values.AgentAnswer{}, cutShort(err)
	}
	if len(p.steps) == 0 {
		p.mu.Unlock()
		return values.AgentAnswer{}, &values.ProviderError{Kind: values.KindPermanent, Reason: "fake provider: no scripted step"}
	}
	step := p.steps[0]
	p.steps = p.steps[1:]
	p.mu.Unlock()

	if step.Entered != nil {
		close(step.Entered)
	}
	if step.Wait != nil {
		select {
		case <-step.Wait:
		case <-ctx.Done():
			return values.AgentAnswer{}, cutShort(ctx.Err())
		}
	}
	if step.Err != nil {
		return values.AgentAnswer{}, step.Err
	}
	return step.Answer, nil
}

// Calls returns a copy of the recorded calls, oldest first.
func (p *FakeAgentProvider) Calls() []values.AgentCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]values.AgentCall(nil), p.calls...)
}
