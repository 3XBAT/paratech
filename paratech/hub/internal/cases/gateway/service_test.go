package gateway_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
)

const (
	alice       shared.APIKeyID = "00000000000000a1"
	bob         shared.APIKeyID = "00000000000000b2"
	ttl                         = 10 * time.Minute
	retention                   = 24 * time.Hour
	callTimeout                 = 5 * time.Minute
	modelCap                    = 1000
	instr                       = "Summarize the lesson."
	data                        = "secret lecture text"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc      *gateway.Service
	store    *testenv.QuotaStore
	provider *testenv.FakeProvider
	limiter  *testenv.FakeLimiter
	clock    *testenv.FakeClock
	logs     *bytes.Buffer
}

func newFixture(t *testing.T, limit int64, steps ...testenv.ProviderStep) fixture {
	t.Helper()
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "default", Model: "claude-test-model", MaxOutput: modelCap}})
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{
		store: testenv.NewQuotaStore(), provider: testenv.NewFakeProvider(steps...), limiter: &testenv.FakeLimiter{},
		clock: testenv.NewFakeClock(t0), logs: &bytes.Buffer{},
	}
	svc, err := gateway.New(gateway.Deps{
		Accounts: f.store, Results: f.store, Provider: f.provider, Limiter: f.limiter, Clock: f.clock, Random: testenv.Random{},
		Models: models, Log: slog.New(slog.NewTextHandler(&syncWriter{w: f.logs}, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}, config(limit))
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

// syncWriter lets the race detector see that log writes and the final read do not overlap.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func config(limit int64) gateway.Config {
	return gateway.Config{Unit: "tokens", Limit: limit, ReservationTTL: ttl, KeyRetention: retention, CallTimeout: callTimeout}
}

func command(owner shared.APIKeyID, key string) gateway.Command {
	return gateway.Command{Owner: owner, IdempotencyKey: key, Operation: "summary", ModelAlias: "default", Instructions: instr, UntrustedData: data}
}

func answer(t *testing.T, output string, in, out int64) testenv.ProviderStep {
	t.Helper()
	u, err := values.NewUsage(in, out)
	if err != nil {
		t.Fatal(err)
	}
	c, err := values.NewCompletion(output, u, values.StopEnd)
	if err != nil {
		t.Fatal(err)
	}
	return testenv.ProviderStep{Completion: c}
}

// amount is what the gateway reserves for command(...) with the alias cap.
func amount(t *testing.T) int64 {
	t.Helper()
	return int64(len(instr)+len(data)) + requestlimits.FramingTokens + modelCap
}

func (f fixture) complete(t *testing.T, cmd gateway.Command) gateway.Result {
	t.Helper()
	res, err := f.svc.Complete(t.Context(), cmd)
	if err != nil {
		t.Fatalf("Complete(%s): %v", cmd.IdempotencyKey, err)
	}
	return res
}

func (f fixture) assertNoText(t *testing.T, secrets ...string) {
	t.Helper()
	for _, s := range append(secrets, instr, data) {
		if strings.Contains(f.logs.String(), s) {
			t.Fatalf("the log contains %q:\n%s", s, f.logs.String())
		}
	}
}

func TestNewValidates(t *testing.T) {
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "a", Model: "m", MaxOutput: 1}})
	if err != nil {
		t.Fatal(err)
	}
	store := testenv.NewQuotaStore()
	full := gateway.Deps{
		Accounts: store, Results: store, Provider: testenv.NewFakeProvider(), Limiter: &testenv.FakeLimiter{},
		Clock: testenv.NewFakeClock(t0), Random: testenv.Random{}, Models: models, Log: slog.New(slog.DiscardHandler),
	}
	if _, err := gateway.New(full, config(10)); err != nil {
		t.Fatalf("valid deps: %v", err)
	}
	broken := map[string]func(*gateway.Deps, *gateway.Config){
		"accounts":          func(d *gateway.Deps, _ *gateway.Config) { d.Accounts = nil },
		"results":           func(d *gateway.Deps, _ *gateway.Config) { d.Results = nil },
		"provider":          func(d *gateway.Deps, _ *gateway.Config) { d.Provider = nil },
		"limiter":           func(d *gateway.Deps, _ *gateway.Config) { d.Limiter = nil },
		"clock":             func(d *gateway.Deps, _ *gateway.Config) { d.Clock = nil },
		"random":            func(d *gateway.Deps, _ *gateway.Config) { d.Random = nil },
		"models":            func(d *gateway.Deps, _ *gateway.Config) { d.Models = nil },
		"log":               func(d *gateway.Deps, _ *gateway.Config) { d.Log = nil },
		"limit":             func(_ *gateway.Deps, c *gateway.Config) { c.Limit = 0 },
		"no call timeout":   func(_ *gateway.Deps, c *gateway.Config) { c.CallTimeout = 0 },
		"timeout above ttl": func(_ *gateway.Deps, c *gateway.Config) { c.CallTimeout = ttl - 9*time.Second },
	}
	for name, mutate := range broken {
		d, c := full, config(10)
		mutate(&d, &c)
		if _, err := gateway.New(d, c); !errors.Is(err, gateway.ErrInvalidConfig) {
			t.Errorf("%s: err = %v, want ErrInvalidConfig", name, err)
		}
	}
}

// TestCompleteSettlesTheActualUsage: the hold is the upper bound, the charge the provider-reported usage (test step 2).
func TestCompleteSettlesTheActualUsage(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, "the untrusted answer", 120, 30)
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 100_000, step)

	done := make(chan gateway.Result, 1)
	failed := make(chan error, 1)
	go func() {
		res, err := f.svc.Complete(t.Context(), command(alice, "job-1"))
		failed <- err
		done <- res
	}()
	<-entered
	if _, held := f.store.Totals(alice); held != amount(t) {
		close(wait)
		t.Fatalf("held during the call = %d, want %d", held, amount(t))
	}
	close(wait)
	if err := <-failed; err != nil {
		t.Fatal(err)
	}
	res := <-done

	if res.Output != "the untrusted answer" || res.InputTokens != 120 || res.OutputTokens != 30 || res.Replayed ||
		shared.ReservationID(res.ReservationID).Validate() != nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Quota != (gateway.Snapshot{Unit: "tokens", Remaining: 100_000 - 150, Reserved: 0, Limit: 100_000}) {
		t.Fatalf("quota = %+v", res.Quota)
	}
	if used, held := f.store.Totals(alice); used != 150 || held != 0 {
		t.Fatalf("used %d held %d, want 150 and 0", used, held)
	}
	calls := f.provider.Calls()
	if len(calls) != 1 || calls[0].Model() != "claude-test-model" || calls[0].MaxOutput() != modelCap ||
		calls[0].Instructions() != instr || calls[0].Data() != data {
		t.Fatalf("provider calls = %v", calls)
	}
	k, _ := f.store.Key(alice, "job-1")
	if !k.Result.Stored || k.Result.Output != "the untrusted answer" || k.Fingerprint.RequestHash == "" || k.Fingerprint.Amount != amount(t) {
		t.Fatalf("key record = %+v", k)
	}
	f.assertNoText(t, "the untrusted answer")
	if !strings.Contains(f.logs.String(), "outcome=completed") || !strings.Contains(f.logs.String(), "input_tokens=120") {
		t.Fatalf("the log lacks the outcome: %s", f.logs.String())
	}
}

func TestCompleteClampsTheOutputCap(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "a", 1, 1), answer(t, "b", 1, 1), answer(t, "c", 1, 1))
	for i, c := range []struct{ requested, want int64 }{{0, modelCap}, {modelCap + 1, modelCap}, {100, 100}} {
		cmd := command(alice, []string{"k0", "k1", "k2"}[i])
		cmd.MaxOutputTokens = c.requested
		f.complete(t, cmd)
		if got := f.provider.Calls()[i].MaxOutput(); got != c.want {
			t.Errorf("max_output_tokens %d: provider got %d, want %d", c.requested, got, c.want)
		}
	}
	if k, _ := f.store.Key(alice, "k2"); k.Fingerprint.Amount != int64(len(instr)+len(data))+requestlimits.FramingTokens+100 {
		t.Fatalf("reserved %d for a cap of 100", k.Fingerprint.Amount)
	}
}

// TestCompleteReplaysTheStoredResult: the same key returns the same answer without a second call or charge (step 4).
func TestCompleteReplaysTheStoredResult(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "answer", 100, 20))
	first := f.complete(t, command(alice, "job-1"))
	ledger := len(f.store.Ledger(alice))

	f.clock.Advance(time.Hour)
	again := f.complete(t, command(alice, "job-1"))
	if !again.Replayed || again.Output != first.Output || again.InputTokens != 100 || again.OutputTokens != 20 ||
		again.ReservationID != first.ReservationID || again.Quota != first.Quota {
		t.Fatalf("replay = %+v, first = %+v", again, first)
	}
	if len(f.provider.Calls()) != 1 || len(f.store.Ledger(alice)) != ledger {
		t.Fatalf("a replay called the provider (%d calls) or wrote the ledger", len(f.provider.Calls()))
	}
	if used, _ := f.store.Totals(alice); used != 120 {
		t.Fatalf("used = %d, want 120", used)
	}

	// Keys are per tester: bob's call with the same text is a new request.
	f.provider.Push(answer(t, "bob's", 1, 1))
	if res := f.complete(t, command(bob, "job-1")); res.Replayed || res.Output != "bob's" {
		t.Fatalf("bob got %+v", res)
	}
}

func TestCompleteConflictsOnAnotherRequest(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "answer", 1, 1))
	f.complete(t, command(alice, "job-1"))

	for name, mutate := range map[string]func(*gateway.Command){
		"data":         func(c *gateway.Command) { c.UntrustedData += "!" },
		"instructions": func(c *gateway.Command) { c.Instructions = "Other." },
		"operation":    func(c *gateway.Command) { c.Operation = "homework_draft" },
		"max output":   func(c *gateway.Command) { c.MaxOutputTokens = 5 },
	} {
		cmd := command(alice, "job-1")
		mutate(&cmd)
		if _, err := f.svc.Complete(t.Context(), cmd); !errors.Is(err, gateway.ErrIdempotencyConflict) {
			t.Errorf("%s: err = %v, want ErrIdempotencyConflict", name, err)
		}
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(f.provider.Calls()))
	}
}

func TestCompleteWhileTheAttemptRuns(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, "answer", 1, 1)
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 100_000, step)

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Complete(context.WithoutCancel(t.Context()), command(alice, "job-1"))
		done <- err
	}()
	<-entered
	if _, err := f.svc.Complete(t.Context(), command(alice, "job-1")); !errors.Is(err, gateway.ErrAttemptRunning) {
		t.Fatalf("err = %v, want ErrAttemptRunning", err)
	}
	other := command(alice, "job-1")
	other.UntrustedData = "other"
	if _, err := f.svc.Complete(t.Context(), other); !errors.Is(err, gateway.ErrIdempotencyConflict) {
		t.Fatalf("another request while running: err = %v, want ErrIdempotencyConflict", err)
	}
	close(wait)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(f.provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(f.provider.Calls()))
	}
}

// TestConcurrentIdenticalCallsMakeOneProviderCall: N callers with one key; one call and one charge.
func TestConcurrentIdenticalCallsMakeOneProviderCall(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, "answer", 10, 5)
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 100_000, step)

	const n = 16
	errs := make([]error, n)
	results := make([]gateway.Result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.svc.Complete(context.WithoutCancel(t.Context()), command(alice, "same"))
		}()
	}
	<-entered // callers that reach the ledger now are aborted, later ones replay
	close(wait)
	wg.Wait()

	completed, replayed, aborted := 0, 0, 0
	for i := range n {
		switch {
		case errs[i] == nil && results[i].Replayed:
			replayed++
		case errs[i] == nil:
			completed++
		case errors.Is(errs[i], gateway.ErrAttemptRunning):
			aborted++
		default:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	if completed != 1 || completed+replayed+aborted != n || len(f.provider.Calls()) != 1 {
		t.Fatalf("completed %d replayed %d aborted %d, provider calls %d", completed, replayed, aborted, len(f.provider.Calls()))
	}
	if used, held := f.store.Totals(alice); used != 15 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestProviderFailureReleasesTheHold: nothing is charged and the next call with the key is a new attempt (step 3).
func TestProviderFailureReleasesTheHold(t *testing.T) {
	transient := &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded", Status: 529, RetryAfter: 7 * time.Second}
	f := newFixture(t, 100_000, testenv.ProviderStep{Err: transient}, answer(t, "answer", 3, 4))

	_, err := f.svc.Complete(t.Context(), command(alice, "job-1"))
	var failed *gateway.ProviderFailedError
	if !errors.As(err, &failed) || !failed.Transient || failed.RetryAfter != 7*time.Second {
		t.Fatalf("err = %v, want a transient ProviderFailedError with retry after 7s", err)
	}
	rs := f.store.Reservations(alice)
	if len(rs) != 1 || rs[0].State != account.StateReleased || rs[0].Reason != account.ReasonFailed || rs[0].Charged != 0 {
		t.Fatalf("reservations = %+v", rs)
	}
	if used, held := f.store.Totals(alice); used != 0 || held != 0 {
		t.Fatalf("used %d held %d after a provider failure", used, held)
	}

	res := f.complete(t, command(alice, "job-1"))
	if res.Replayed || res.Output != "answer" || res.ReservationID == rs[0].ID.String() || len(f.provider.Calls()) != 2 {
		t.Fatalf("retry = %+v, provider calls %d", res, len(f.provider.Calls()))
	}
}

func TestPermanentProviderFailures(t *testing.T) {
	refusal := answer(t, "", 10, 0)
	u, _ := values.NewUsage(10, 0)
	refusal.Completion, _ = values.NewCompletion("", u, values.StopRefusal)
	f := newFixture(t, 100_000,
		testenv.ProviderStep{Err: &values.ProviderError{Kind: values.KindPermanent, Reason: "rejected", Status: 401, Type: "authentication_error"}},
		refusal,
		testenv.ProviderStep{Err: errors.New("not a provider error")},
	)
	for i := range 3 {
		_, err := f.svc.Complete(t.Context(), command(alice, []string{"k1", "k2", "k3"}[i]))
		var failed *gateway.ProviderFailedError
		if !errors.As(err, &failed) || failed.Transient {
			t.Fatalf("call %d: err = %v, want a permanent ProviderFailedError", i+1, err)
		}
	}
	if used, held := f.store.Totals(alice); used != 0 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

func TestRateLimitedCallReservesNothing(t *testing.T) {
	f := newFixture(t, 100_000)
	f.limiter.Refuse(3 * time.Second)
	_, err := f.svc.Complete(t.Context(), command(alice, "job-1"))
	var limited *gateway.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 3*time.Second {
		t.Fatalf("err = %v, want RateLimitedError", err)
	}
	if len(f.store.Ledger(alice)) != 0 || len(f.provider.Calls()) != 0 {
		t.Fatal("a rate-limited call reached the ledger or the provider")
	}
	if !strings.Contains(f.logs.String(), "outcome=rate_limited") {
		t.Fatalf("log: %s", f.logs.String())
	}
}

func TestQuotaExceededCallsNoProvider(t *testing.T) {
	f := newFixture(t, amount(t)-1)
	_, err := f.svc.Complete(t.Context(), command(alice, "job-1"))
	var exceeded *gateway.QuotaExceededError
	if !errors.As(err, &exceeded) || !errors.Is(err, account.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want QuotaExceededError", err)
	}
	if exceeded.Quota != (gateway.Snapshot{Unit: "tokens", Remaining: amount(t) - 1, Limit: amount(t) - 1}) {
		t.Fatalf("snapshot = %+v", exceeded.Quota)
	}
	if len(f.provider.Calls()) != 0 || len(f.store.Ledger(alice)) != 0 {
		t.Fatal("QUOTA_EXCEEDED called the provider or wrote the ledger")
	}
}

func TestInvalidRequests(t *testing.T) {
	f := newFixture(t, 100_000)
	for name, mutate := range map[string]func(*gateway.Command){
		"owner":         func(c *gateway.Command) { c.Owner = "x" },
		"key":           func(c *gateway.Command) { c.IdempotencyKey = "with space" },
		"operation":     func(c *gateway.Command) { c.Operation = "" },
		"unknown alias": func(c *gateway.Command) { c.ModelAlias = "other" },
		"bad alias":     func(c *gateway.Command) { c.ModelAlias = "Default" },
		"instructions":  func(c *gateway.Command) { c.Instructions = "" },
		"NUL":           func(c *gateway.Command) { c.UntrustedData = "\x00" },
		"too large":     func(c *gateway.Command) { c.UntrustedData = strings.Repeat("d", values.MaxPayloadBytes) },
		"max output":    func(c *gateway.Command) { c.MaxOutputTokens = -1 },
	} {
		cmd := command(alice, "job-1")
		mutate(&cmd)
		if _, err := f.svc.Complete(t.Context(), cmd); !errors.Is(err, gateway.ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}
	if len(f.store.Ledger(alice)) != 0 || len(f.provider.Calls()) != 0 {
		t.Fatal("an invalid request reached the ledger or the provider")
	}
}

func TestClientGoneBeforeTheProviderCallReleases(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "never", 1, 1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // the fake store ignores the context, so Start succeeds and the gateway sees the cancel afterwards
	if _, err := f.svc.Complete(ctx, command(alice, "job-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	rs := f.store.Reservations(alice)
	if len(rs) != 1 || rs[0].State != account.StateReleased || rs[0].Reason != account.ReasonExplicit || len(f.provider.Calls()) != 0 {
		t.Fatalf("reservations %+v, provider calls %d", rs, len(f.provider.Calls()))
	}
}

// cancelDuringCommit commits the first Modify, then cancels the client and reports the context error of the context
// it was given, as a driver does when the client cancels during COMMIT after the commit landed.
type cancelDuringCommit struct {
	*testenv.QuotaStore
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelDuringCommit) Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error {
	err := c.QuotaStore.Modify(ctx, scope, fn)
	first := false
	c.once.Do(func() { first = true; c.cancel() })
	if err == nil && first {
		return ctx.Err()
	}
	return err
}

// TestClientGoneDuringTheStartCommit: a committed hold is released, not left RESERVED until its TTL.
func TestClientGoneDuringTheStartCommit(t *testing.T) {
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "default", Model: "claude-test-model", MaxOutput: modelCap}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := testenv.NewQuotaStore()
	provider := testenv.NewFakeProvider(answer(t, "never", 1, 1))
	svc, err := gateway.New(gateway.Deps{
		Accounts: &cancelDuringCommit{QuotaStore: store, cancel: cancel}, Results: store, Provider: provider,
		Limiter: &testenv.FakeLimiter{}, Clock: testenv.NewFakeClock(t0), Random: testenv.Random{}, Models: models,
		Log: slog.New(slog.DiscardHandler),
	}, config(100_000))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Complete(ctx, command(alice, "job-1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	rs := store.Reservations(alice)
	if len(rs) != 1 || rs[0].State != account.StateReleased || rs[0].Reason != account.ReasonExplicit || len(provider.Calls()) != 0 {
		t.Fatalf("reservations %+v, provider calls %d", rs, len(provider.Calls()))
	}
	if used, held := store.Totals(alice); used != 0 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestClientGoneDuringTheCall: the call finishes on the detached context, is settled and stored; a retry replays it.
func TestClientGoneDuringTheCall(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, "paid answer", 40, 2)
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 100_000, step)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Complete(ctx, command(alice, "job-1"))
		done <- err
	}()
	<-entered
	cancel()
	close(wait)
	if err := <-done; err != nil {
		t.Fatalf("the started call failed after the cancel: %v", err)
	}
	if used, held := f.store.Totals(alice); used != 42 || held != 0 {
		t.Fatalf("used %d held %d, want the call settled", used, held)
	}
	res := f.complete(t, command(alice, "job-1"))
	if !res.Replayed || res.Output != "paid answer" || len(f.provider.Calls()) != 1 {
		t.Fatalf("retry = %+v, provider calls %d", res, len(f.provider.Calls()))
	}
}

func TestProviderCallIsBoundedByTheTimeout(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "a", 1, 1))
	f.complete(t, command(alice, "job-1"))
	budget := f.provider.Budgets()[0]
	if budget <= callTimeout-time.Minute || budget > callTimeout {
		t.Fatalf("provider call budget = %s, want just under %s", budget, callTimeout)
	}
}

func TestSettlementFailureKeepsTheHoldUntilExpiry(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := answer(t, "lost answer", 1, 1)
	step.Wait, step.Entered = wait, entered
	f := newFixture(t, 100_000, step)

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Complete(t.Context(), command(alice, "job-1"))
		done <- err
	}()
	<-entered
	f.store.Err = errors.New("database is down")
	close(wait)
	err := <-done
	if err == nil || errors.Is(err, gateway.ErrInvalidRequest) {
		t.Fatalf("err = %v, want a Hub failure", err)
	}
	f.store.Err = nil
	if rs := f.store.Reservations(alice); len(rs) != 1 || rs[0].State != account.StateReserved {
		t.Fatalf("reservations = %+v", rs)
	}
	f.assertNoText(t, "lost answer")
}

// TestStoredResultEndsWithTheResultWindow: the answer is replayed until the result window ends (ResultPurgeInterval
// before the retention), then never returned again; the purge clears it.
func TestStoredResultEndsWithTheResultWindow(t *testing.T) {
	f := newFixture(t, 100_000, answer(t, "old answer", 1, 1), answer(t, "new answer", 1, 1))
	f.complete(t, command(alice, "job-1"))

	f.clock.Advance(retention - account.ResultPurgeInterval - time.Nanosecond)
	if n, err := f.svc.PurgeResults(t.Context()); err != nil || n != 0 {
		t.Fatalf("PurgeResults within the window = %d, %v; want 0", n, err)
	}
	if res := f.complete(t, command(alice, "job-1")); !res.Replayed || res.Output != "old answer" {
		t.Fatalf("within the window = %+v", res)
	}

	f.clock.Advance(time.Nanosecond)
	if n, err := f.svc.PurgeResults(t.Context()); err != nil || n != 1 {
		t.Fatalf("PurgeResults = %d, %v; want 1", n, err)
	}
	if k, _ := f.store.Key(alice, "job-1"); k.Result.Stored {
		t.Fatal("the purge kept the result")
	}
	res := f.complete(t, command(alice, "job-1"))
	if res.Replayed || res.Output != "new answer" || len(f.provider.Calls()) != 2 {
		t.Fatalf("after the window = %+v", res)
	}
	if n, err := f.svc.PurgeResults(t.Context()); err != nil || n != 0 {
		t.Fatalf("PurgeResults of a fresh result = %d, %v; want 0", n, err)
	}
}
