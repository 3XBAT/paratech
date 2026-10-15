package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/hub/config"
	"paratech/hub/internal/adapters/anthropic"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/testenv"
)

// The adapter and the fake implement the gateway's Provider port.
var (
	_ gateway.Provider = (*anthropic.Client)(nil)
	_ gateway.Provider = (*testenv.FakeProvider)(nil)
)

// providerTestKey is an obviously fake provider key.
const providerTestKey = "test-key-not-real"

// allowlist builds the domain allowlist from an allowlist spec the way the Hub configuration provides it.
func allowlist(t *testing.T, spec string) *modelallowlist.List {
	t.Helper()
	models, err := config.ParseModels(spec)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]modelallowlist.Entry, 0, len(models))
	for _, m := range models {
		entries = append(entries, modelallowlist.Entry{Alias: m.Alias, Model: m.ID, MaxOutput: m.MaxOutput})
	}
	list, err := modelallowlist.New(entries)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// callProvider is the provider part of the gateway path: the client names only an alias, the allowlist resolves it.
func callProvider(ctx context.Context, list *modelallowlist.List, p gateway.Provider, alias string, maxOutput int64,
	instructions, data string) (values.Completion, error) {
	model, effective, err := list.Resolve(alias, maxOutput)
	if err != nil {
		return values.Completion{}, err
	}
	req, err := values.NewRequest(model, instructions, data, effective)
	if err != nil {
		return values.Completion{}, err
	}
	return p.Complete(ctx, req)
}

// fakeAnthropic is a local Messages API that records the model and max_tokens of each call.
type fakeAnthropic struct {
	srv    *httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	models []string
	caps   []int64
}

func newFakeAnthropic(t *testing.T) *fakeAnthropic {
	t.Helper()
	f := &fakeAnthropic{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		var body struct {
			Model     string `json:"model"`
			MaxTokens int64  `json:"max_tokens"`
		}
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != providerTestKey || json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.models, f.caps = append(f.models, body.Model), append(f.caps, body.MaxTokens)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"`+body.Model+`",
			"content":[{"type":"thinking","thinking":"..."},{"type":"text","text":"Summary."}],"stop_reason":"end_turn",
			"usage":{"input_tokens":1200,"output_tokens":150,"cache_creation_input_tokens":null,"cache_read_input_tokens":30}}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAnthropic) client(t *testing.T) *anthropic.Client {
	t.Helper()
	c, err := anthropic.New(anthropic.Options{APIKey: providerTestKey, BaseURL: f.srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDefaultAllowlistIsValid(t *testing.T) {
	list := allowlist(t, config.DefaultLLMModels)
	model, maxOutput, err := list.Resolve("default", 0)
	if err != nil || model != "claude-sonnet-5-5" || maxOutput != 8192 {
		t.Fatalf("Resolve(default) = %q, %d, %v", model, maxOutput, err)
	}
}

// The alias resolves through the allowlist, the adapter sends the resolved model and the clamped cap, and the
// provider-reported usage comes back for settlement.
func TestProviderScenarioThroughTheAllowlist(t *testing.T) {
	f := newFakeAnthropic(t)
	list := allowlist(t, "default=claude-sonnet-5-5:8192,fast=claude-haiku-5-5:1024")
	p := f.client(t)

	got, err := callProvider(t.Context(), list, p, "fast", 50_000, "Summarize the lesson.", "transcript excerpt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Output() != "Summary." || got.Stop() != values.StopEnd {
		t.Fatalf("completion = %v", got)
	}
	if got.Usage().Input() != 1230 || got.Usage().Output() != 150 || got.Usage().Total() != 1380 {
		t.Fatalf("usage = %d/%d, want 1230/150", got.Usage().Input(), got.Usage().Output())
	}

	if _, err := callProvider(t.Context(), list, p, "default", 0, "Summarize the lesson.", ""); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if fmt.Sprint(f.models) != "[claude-haiku-5-5 claude-sonnet-5-5]" || fmt.Sprint(f.caps) != "[1024 8192]" {
		t.Fatalf("provider saw models %v caps %v", f.models, f.caps)
	}
}

// Test step 2: a model outside the allowlist is rejected before any provider call.
func TestProviderNonAllowedModelNeverReachesTheProvider(t *testing.T) {
	f := newFakeAnthropic(t)
	list := allowlist(t, config.DefaultLLMModels)
	p := f.client(t)

	for _, alias := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "opus", "https://evil.example/v1/messages", "DEFAULT", ""} {
		_, err := callProvider(t.Context(), list, p, alias, 100, "Summarize.", "data")
		if !errors.Is(err, modelallowlist.ErrModelNotAllowed) {
			t.Errorf("alias %q: error = %v, want ErrModelNotAllowed", alias, err)
		}
	}
	if f.hits.Load() != 0 {
		t.Fatalf("the provider was called %d times", f.hits.Load())
	}
}

// Test step 3: the fake provider returns usage, and a call cut short by a timeout is a transient error.
func TestFakeProviderUsageAndTimeout(t *testing.T) {
	usage, err := values.NewUsage(900, 120)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := values.NewCompletion("fake output", usage, values.StopEnd)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	fake := testenv.NewFakeProvider(
		testenv.ProviderStep{Completion: completion},
		testenv.ProviderStep{Block: true, Entered: entered},
		testenv.ProviderStep{Err: &values.ProviderError{Kind: values.KindPermanent, Reason: "credentials rejected", Status: 401}},
	)
	list := allowlist(t, config.DefaultLLMModels)

	got, err := callProvider(t.Context(), list, fake, "default", 0, "Summarize.", "data")
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage().Input() != 900 || got.Usage().Output() != 120 || got.Usage().Total() != 1020 {
		t.Fatalf("usage = %v", got)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = callProvider(ctx, list, fake, "default", 0, "Summarize.", "data")
	<-entered
	if !errors.Is(err, values.ErrProviderTransient) || errors.Is(err, values.ErrProviderPermanent) {
		t.Fatalf("blocked call: error = %v, want transient", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked call: error = %v, want the context deadline as cause", err)
	}

	_, err = callProvider(t.Context(), list, fake, "default", 0, "Summarize.", "data")
	if !errors.Is(err, values.ErrProviderPermanent) {
		t.Fatalf("scripted error = %v, want permanent", err)
	}
	_, err = callProvider(t.Context(), list, fake, "default", 0, "Summarize.", "data")
	if !errors.Is(err, values.ErrProviderPermanent) {
		t.Fatalf("unscripted call: error = %v, want permanent", err)
	}

	calls := fake.Calls()
	if len(calls) != 4 || calls[0].Model() != "claude-sonnet-5-5" || calls[0].MaxOutput() != 8192 || calls[0].Data() != "data" {
		t.Fatalf("recorded calls = %v", calls)
	}
}

func TestFakeProviderCancelledContext(t *testing.T) {
	fake := testenv.NewFakeProvider(testenv.ProviderStep{Block: true})
	req, err := values.NewRequest("m", "i", "", 1)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = fake.Complete(ctx, req)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, values.ErrProviderTransient) {
		t.Fatalf("error = %v, want a transient cancelled error", err)
	}
	if fake.Remaining() != 1 {
		t.Fatal("a call on an ended context must not consume a step")
	}
}

func TestFakeProviderConcurrentCalls(t *testing.T) {
	usage, _ := values.NewUsage(1, 1)
	completion, _ := values.NewCompletion("x", usage, values.StopEnd)
	const n = 32
	fake := testenv.NewFakeProvider()
	for range n {
		fake.Push(testenv.ProviderStep{Completion: completion})
	}
	req, err := values.NewRequest("m", "i", "", 1)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var failed atomic.Int32
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fake.Complete(t.Context(), req); err != nil {
				failed.Add(1)
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 || len(fake.Calls()) != n || fake.Remaining() != 0 {
		t.Fatalf("failed %d, calls %d, remaining %d", failed.Load(), len(fake.Calls()), fake.Remaining())
	}
}
