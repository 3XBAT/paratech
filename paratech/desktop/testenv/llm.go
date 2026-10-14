package testenv

import (
	"context"
	"errors"
	"sync"

	"paratech/desktop/internal/domain/coursework/values"
)

// ErrNoReply is returned by FakeLLM for an operation it has no reply for.
var ErrNoReply = errors.New("fake llm: no reply scripted")

// LLMReply is one scripted reply of FakeLLM: the model output, or an error such as shared.ErrQuotaExceeded.
type LLMReply struct {
	Output string
	Err    error
}

// FakeLLM is the LLM Client of the use cases (homeworkflow.LLMClient) without a Hub. Replies are scripted per
// operation and used in order; the last one repeats until more are added. It is "obedient": it returns whatever it
// was told, as a model that followed an injected instruction would, so the core's checks are what is tested. Every
// request is recorded.
type FakeLLM struct {
	mu       sync.Mutex
	replies  map[values.Operation][]LLMReply
	used     map[values.Operation]int // replies of the operation already returned
	requests []values.LLMRequest
}

// NewFakeLLM returns a fake without replies; an operation without replies fails with ErrNoReply.
func NewFakeLLM() *FakeLLM {
	return &FakeLLM{replies: map[values.Operation][]LLMReply{}, used: map[values.Operation]int{}}
}

// Reply appends replies for operation.
func (f *FakeLLM) Reply(op values.Operation, replies ...LLMReply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[op] = append(f.replies[op], replies...)
}

// Answer appends one model output for operation.
func (f *FakeLLM) Answer(op values.Operation, output string) { f.Reply(op, LLMReply{Output: output}) }

// Complete records req and returns the next reply of its operation.
func (f *FakeLLM) Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error) {
	if err := ctx.Err(); err != nil {
		return values.LLMResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)

	op := req.Operation()
	queue := f.replies[op]
	if len(queue) == 0 {
		return values.LLMResult{}, ErrNoReply
	}
	r := queue[min(f.used[op], len(queue)-1)]
	f.used[op]++
	if r.Err != nil {
		return values.LLMResult{}, r.Err
	}
	return values.NewLLMResult(values.NewUntrustedOutput(r.Output), int64(len(req.UntrustedData())), int64(len(r.Output)))
}

// Requests returns the recorded requests, in order.
func (f *FakeLLM) Requests() []values.LLMRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]values.LLMRequest(nil), f.requests...)
}

// RequestsFor returns the recorded requests of one operation.
func (f *FakeLLM) RequestsFor(op values.Operation) []values.LLMRequest {
	var out []values.LLMRequest
	for _, r := range f.Requests() {
		if r.Operation() == op {
			out = append(out, r)
		}
	}
	return out
}
