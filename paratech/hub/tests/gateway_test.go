package tests

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/adapters/ratelimit"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/services/requestlimits"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// The Postgres adapter and the in-memory limiter implement the ports of the gateway use case.
var (
	_ gateway.Accounts    = (*postgres.QuotaAccounts)(nil)
	_ gateway.Results     = (*postgres.QuotaAccounts)(nil)
	_ gateway.RateLimiter = (*ratelimit.Limiter)(nil)
)

// Texts that must never reach a log or a stored row (the output is stored on purpose, as bytes, and never logged).
const (
	gwInstructions = "instructions-marker: summarize the lesson"
	gwData         = "lecture-marker: the student said something private"
	gwOutput       = "output-marker: the summary"
	gwModelCap     = 1000
	gwRetention    = 24 * time.Hour
)

// gatewayEnv is the real Hub path on a fresh Postgres schema: router with API-key auth, gateway handler, gateway use
// case, quota ledger and the in-memory rate limiter. Only the provider and the clock are fakes.
type gatewayEnv struct {
	quotaEnv
	svc      *gateway.Service
	provider *testenv.FakeProvider
	client   hubv1connect.GatewayServiceClient
	quotas   hubv1connect.QuotaServiceClient
	owner    shared.APIKeyID
	apiKey   string
	logs     *lockedBuffer
}

type gatewayOptions struct {
	limit            int64
	perMinute, burst int
	wrap             func(gateway.Accounts) gateway.Accounts
}

// lockedBuffer is a log sink safe for the handler goroutines of the test server.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newGatewayEnv(t *testing.T, o gatewayOptions, steps ...testenv.ProviderStep) gatewayEnv {
	t.Helper()
	if o.limit == 0 {
		o.limit = 1_000_000
	}
	if o.perMinute == 0 {
		o.perMinute, o.burst = requestlimits.MaxPerMinute, requestlimits.MaxBurst
	}
	e := gatewayEnv{quotaEnv: newQuotaEnv(t, o.limit), provider: testenv.NewFakeProvider(steps...), logs: &lockedBuffer{}}
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	keySvc, err := keys.New(e.db.APIKeys(), e.clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := keySvc.Issue(t.Context(), "gateway-tester")
	if err != nil {
		t.Fatal(err)
	}
	e.owner, e.apiKey = shared.APIKeyID(issued.ID), issued.Key

	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "default", Model: "claude-test-model", MaxOutput: gwModelCap}})
	if err != nil {
		t.Fatal(err)
	}
	rate, err := requestlimits.NewRate(o.perMinute, o.burst)
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := ratelimit.New(rate)
	if err != nil {
		t.Fatal(err)
	}
	var accounts gateway.Accounts = e.db.QuotaAccounts()
	if o.wrap != nil {
		accounts = o.wrap(accounts)
	}
	e.svc, err = gateway.New(gateway.Deps{
		Accounts: accounts, Results: e.db.QuotaAccounts(), Provider: e.provider, Limiter: limiter, Clock: e.clock,
		Random: testenv.Random{}, Models: models, Log: log,
	}, gateway.Config{Unit: "tokens", Limit: o.limit, ReservationTTL: quotaTTL, KeyRetention: gwRetention, CallTimeout: 4 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	auth, err := keyauth.New(e.db.APIKeys())
	if err != nil {
		t.Fatal(err)
	}
	gh, err := connect.NewGateway(e.svc, log)
	if err != nil {
		t.Fatal(err)
	}
	qh, err := connect.NewQuota(e.quotaEnv.svc, log)
	if err != nil {
		t.Fatal(err)
	}
	router, err := connect.NewRouter(connect.Deps{Log: log, Readiness: e.db, Auth: auth, Gateway: gh, Quota: qh})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	e.client = hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	e.quotas = hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL)
	return e
}

func gwSpec(key string) *hubv1.CompletionSpec {
	return &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_SUMMARY, ModelAlias: "default",
		Instructions: gwInstructions, UntrustedData: gwData,
	}
}

func (e gatewayEnv) complete(ctx context.Context, spec *hubv1.CompletionSpec) (*hubv1.CompletionResult, error) {
	req := connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: spec})
	req.Header().Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetResult(), nil
}

func (e gatewayEnv) mustComplete(t *testing.T, spec *hubv1.CompletionSpec) *hubv1.CompletionResult {
	t.Helper()
	res, err := e.complete(t.Context(), spec)
	if err != nil {
		t.Fatalf("Complete(%s): %v", spec.GetIdempotencyKey(), err)
	}
	return res
}

func (e gatewayEnv) remaining(t *testing.T) *hubv1.QuotaSnapshot {
	t.Helper()
	req := connectrpc.NewRequest(&hubv1.GetRemainingRequest{})
	req.Header().Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.quotas.GetRemaining(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.Msg.GetQuota()
}

// gwAmount is what the gateway reserves for gwSpec with the alias cap.
func gwAmount() int64 {
	return int64(len(gwInstructions)+len(gwData)) + requestlimits.FramingTokens + gwModelCap
}

func gwAnswer(t *testing.T, output string, in, out int64) testenv.ProviderStep {
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

// gwDetail returns the Connect code and the ErrorDetail of err (nil when there is none).
func gwDetail(t *testing.T, err error) (connectrpc.Code, *hubv1.ErrorDetail) {
	t.Helper()
	var ce *connectrpc.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not a Connect error", err)
	}
	for _, d := range ce.Details() {
		v, err := d.Value()
		if err != nil {
			t.Fatal(err)
		}
		if detail, ok := v.(*hubv1.ErrorDetail); ok {
			return ce.Code(), detail
		}
	}
	return ce.Code(), nil
}

// reservationStates returns "state/reason charged" of every reservation of the tester, oldest first.
func (e gatewayEnv) reservationStates(t *testing.T) []string {
	t.Helper()
	rows, err := e.conn.Query(t.Context(), "SELECT state || '/' || release_reason || ' ' || charged FROM quota_reservations "+
		"WHERE api_key_id = $1 ORDER BY created_at, attempt", e.owner.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// assertNoRequestText checks that no stored row holds the request text (only its hash is kept) and that the log has
// neither the request text, the output nor the API key.
func (e gatewayEnv) assertNoRequestText(t *testing.T) {
	t.Helper()
	for _, table := range []string{"quota_accounts", "quota_reservations", "quota_idempotency_keys", "quota_ledger", "api_keys"} {
		var n int
		err := e.conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" t WHERE t::text LIKE '%marker%' OR t::text LIKE '%' || $1 || '%'",
			e.apiKey).Scan(&n)
		if err != nil || n != 0 {
			t.Fatalf("%s: %d rows hold request text or the API key (%v)", table, n, err)
		}
	}
	logs := e.logs.String()
	for _, secret := range []string{gwInstructions, gwData, gwOutput, "marker", e.apiKey} {
		if strings.Contains(logs, secret) {
			t.Fatalf("the log contains %q:\n%s", secret, logs)
		}
	}
}

// TestGatewaySettlesTheActualUsageAgainstPostgres is test step 2: the hold is the upper bound, the charge is the
// provider-reported usage, and the quota and the ledger show exactly that.
func TestGatewaySettlesTheActualUsageAgainstPostgres(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := gwAnswer(t, gwOutput, 120, 30)
	step.Wait, step.Entered = wait, entered
	e := newGatewayEnv(t, gatewayOptions{}, step)

	type outcome struct {
		res *hubv1.CompletionResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := e.complete(t.Context(), gwSpec("job-1"))
		done <- outcome{res, err}
	}()
	<-entered
	if q := e.remaining(t); q.GetReserved().GetValue() != gwAmount() {
		close(wait)
		t.Fatalf("held during the call = %d, want %d", q.GetReserved().GetValue(), gwAmount())
	}
	close(wait)
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}

	res := got.res
	if res.GetOutput() != gwOutput || res.GetUsage().GetInputTokens() != 120 || res.GetUsage().GetOutputTokens() != 30 || res.GetReplayed() ||
		res.GetQuota().GetRemaining().GetValue() != 1_000_000-150 || res.GetQuota().GetReserved().GetValue() != 0 {
		t.Fatalf("result = %v", res)
	}
	if q := e.remaining(t); q.GetRemaining().GetValue() != 1_000_000-150 {
		t.Fatalf("GetRemaining = %v", q)
	}
	if got := e.reservationStates(t); len(got) != 1 || got[0] != "settled/ 150" {
		t.Fatalf("reservations = %v", got)
	}
	if kinds := e.ledgerKinds(t, e.owner); kinds["reserve"] != 1 || kinds["settle"] != 1 || len(kinds) != 2 {
		t.Fatalf("ledger = %v", kinds)
	}
	if used, held := e.assertConsistent(t, e.owner); used != 150 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	if calls := e.provider.Calls(); len(calls) != 1 || calls[0].MaxOutput() != gwModelCap || calls[0].Model() != "claude-test-model" {
		t.Fatalf("provider calls = %v", calls)
	}
	var hash string
	if err := e.conn.QueryRow(t.Context(), "SELECT request_hash FROM quota_idempotency_keys WHERE api_key_id = $1", e.owner.String()).
		Scan(&hash); err != nil || len(hash) != 64 {
		t.Fatalf("request hash %q, %v", hash, err)
	}
	e.assertNoRequestText(t)
}

// TestGatewayProviderFailureReleasesAgainstPostgres is test step 3: the hold goes back, nothing is charged, and a
// retry with the same key is a new attempt.
func TestGatewayProviderFailureReleasesAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{},
		testenv.ProviderStep{Err: &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded", Status: 529, RetryAfter: 5 * time.Second}},
		testenv.ProviderStep{Err: &values.ProviderError{Kind: values.KindPermanent, Reason: "rejected", Status: 401, Type: "authentication_error"}},
		gwAnswer(t, gwOutput, 10, 5),
	)
	_, err := e.complete(t.Context(), gwSpec("job-1"))
	code, d := gwDetail(t, err)
	if code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || !d.GetRetryable() ||
		d.GetRetryAfter().AsDuration() != 5*time.Second {
		t.Fatalf("transient failure: %v / %v", code, d)
	}
	// A provider 401 is the Hub's problem, never the tester's key.
	_, err = e.complete(t.Context(), gwSpec("job-1"))
	if code, d := gwDetail(t, err); code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR || d.GetRetryable() {
		t.Fatalf("permanent failure: %v / %v", code, d)
	}
	if q := e.remaining(t); q.GetRemaining().GetValue() != 1_000_000 || q.GetReserved().GetValue() != 0 {
		t.Fatalf("quota after failures = %v", q)
	}
	if got := e.reservationStates(t); len(got) != 2 || got[0] != "released/failed 0" || got[1] != "released/failed 0" {
		t.Fatalf("reservations = %v", got)
	}

	if res := e.mustComplete(t, gwSpec("job-1")); res.GetReplayed() || res.GetOutput() != gwOutput {
		t.Fatalf("retry = %v", res)
	}
	if used, held := e.assertConsistent(t, e.owner); used != 15 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	e.assertNoRequestText(t)
}

// TestGatewayReplayAgainstPostgres is test step 4: the same key returns the same answer without a second provider
// call or charge; another request with the key is IDEMPOTENCY_CONFLICT, also after completion.
func TestGatewayReplayAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{}, gwAnswer(t, gwOutput, 100, 20))
	first := e.mustComplete(t, gwSpec("job-1"))
	ledger := e.rowCount(t, "quota_ledger", e.owner)

	e.clock.Advance(time.Hour)
	again := e.mustComplete(t, gwSpec("job-1"))
	if !again.GetReplayed() || again.GetOutput() != first.GetOutput() || again.GetReservationId() != first.GetReservationId() ||
		again.GetUsage().GetInputTokens() != 100 || again.GetUsage().GetOutputTokens() != 20 {
		t.Fatalf("replay = %v, first = %v", again, first)
	}
	if len(e.provider.Calls()) != 1 || e.rowCount(t, "quota_ledger", e.owner) != ledger {
		t.Fatalf("the replay called the provider (%d calls) or wrote the ledger", len(e.provider.Calls()))
	}
	if used, _ := e.assertConsistent(t, e.owner); used != 120 {
		t.Fatalf("used = %d, want one charge of 120", used)
	}

	other := gwSpec("job-1")
	other.UntrustedData += " (edited)"
	_, err := e.complete(t.Context(), other)
	if code, d := gwDetail(t, err); code != connectrpc.CodeFailedPrecondition || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT || d.GetRetryable() {
		t.Fatalf("conflict after completion: %v / %v", code, d)
	}
	if len(e.provider.Calls()) != 1 {
		t.Fatal("a conflicting request reached the provider")
	}
	e.assertNoRequestText(t)
}

func TestGatewayConflictWhileRunningAgainstPostgres(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := gwAnswer(t, gwOutput, 1, 1)
	step.Wait, step.Entered = wait, entered
	e := newGatewayEnv(t, gatewayOptions{}, step)

	done := make(chan error, 1)
	go func() {
		_, err := e.complete(t.Context(), gwSpec("job-1"))
		done <- err
	}()
	<-entered
	other := gwSpec("job-1")
	other.Instructions = "other instructions"
	_, conflict := e.complete(t.Context(), other)
	_, running := e.complete(t.Context(), gwSpec("job-1"))
	close(wait)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if code, d := gwDetail(t, conflict); code != connectrpc.CodeFailedPrecondition || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
		t.Fatalf("conflict while running: %v / %v", code, d)
	}
	if code, d := gwDetail(t, running); code != connectrpc.CodeAborted || d != nil {
		t.Fatalf("same request while running: %v / %v, want aborted without detail", code, d)
	}
}

// TestGatewayConcurrentIdenticalCallsAgainstPostgres: N callers with one key make one provider call and one charge.
func TestGatewayConcurrentIdenticalCallsAgainstPostgres(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := gwAnswer(t, gwOutput, 10, 5)
	step.Wait, step.Entered = wait, entered
	e := newGatewayEnv(t, gatewayOptions{}, step)

	const n = 12
	results := make([]*hubv1.CompletionResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = e.complete(t.Context(), gwSpec("same"))
		}()
	}
	<-entered
	close(wait)
	wg.Wait()

	completed, replayed, aborted := 0, 0, 0
	for i := range n {
		switch {
		case errs[i] == nil && results[i].GetReplayed():
			replayed++
		case errs[i] == nil:
			completed++
		case connectrpc.CodeOf(errs[i]) == connectrpc.CodeAborted:
			aborted++
		default:
			t.Fatalf("caller %d: %v", i, errs[i])
		}
	}
	if completed != 1 || completed+replayed+aborted != n || len(e.provider.Calls()) != 1 {
		t.Fatalf("completed %d replayed %d aborted %d, provider calls %d", completed, replayed, aborted, len(e.provider.Calls()))
	}
	if used, held := e.assertConsistent(t, e.owner); used != 15 || held != 0 || e.rowCount(t, "quota_reservations", e.owner) != 1 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestGatewayClientGoneBeforeTheProviderCallAgainstPostgres: the hold made for a client that left is released whole.
func TestGatewayClientGoneBeforeTheProviderCallAgainstPostgres(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e := newGatewayEnv(t, gatewayOptions{wrap: func(a gateway.Accounts) gateway.Accounts {
		return &testenv.AfterFirstModify{Accounts: a, Hook: cancel} // the client leaves right after the reservation
	}}, gwAnswer(t, gwOutput, 1, 1))

	_, err := e.svc.Complete(ctx, gateway.Command{
		Owner: e.owner, IdempotencyKey: "job-1", Operation: "summary", ModelAlias: "default", Instructions: gwInstructions, UntrustedData: gwData,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := e.reservationStates(t); len(got) != 1 || got[0] != "released/explicit 0" || len(e.provider.Calls()) != 0 {
		t.Fatalf("reservations %v, provider calls %d", got, len(e.provider.Calls()))
	}
	if used, held := e.assertConsistent(t, e.owner); used != 0 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestGatewayClientGoneDuringTheCallAgainstPostgres: a started call finishes, is settled and stored although the
// client left; the retry replays what was paid for.
func TestGatewayClientGoneDuringTheCallAgainstPostgres(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := gwAnswer(t, gwOutput, 40, 2)
	step.Wait, step.Entered = wait, entered
	e := newGatewayEnv(t, gatewayOptions{}, step)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := e.complete(ctx, gwSpec("job-1"))
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; connectrpc.CodeOf(err) != connectrpc.CodeCanceled {
		t.Fatalf("client error = %v, want canceled", err)
	}
	close(wait)

	// The server finishes on its own; wait for the settlement (bounded).
	deadline := time.Now().Add(15 * time.Second)
	for got := e.reservationStates(t); len(got) != 1 || got[0] != "settled/ 42"; got = e.reservationStates(t) {
		if time.Now().After(deadline) {
			t.Fatalf("the started call was not settled: %v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	res := e.mustComplete(t, gwSpec("job-1"))
	if !res.GetReplayed() || res.GetOutput() != gwOutput || len(e.provider.Calls()) != 1 {
		t.Fatalf("retry = %v, provider calls %d", res, len(e.provider.Calls()))
	}
	if used, _ := e.assertConsistent(t, e.owner); used != 42 {
		t.Fatalf("used = %d, want 42", used)
	}
}

// TestGatewayRateLimitAgainstPostgres: over the per-key limit the call is RATE_LIMITED and nothing is reserved.
func TestGatewayRateLimitAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{perMinute: 1, burst: 2}, gwAnswer(t, gwOutput, 1, 1), gwAnswer(t, gwOutput, 1, 1))
	e.mustComplete(t, gwSpec("a"))
	e.mustComplete(t, gwSpec("b"))
	before := e.rowCount(t, "quota_ledger", e.owner)

	_, err := e.complete(t.Context(), gwSpec("c"))
	code, d := gwDetail(t, err)
	if code != connectrpc.CodeResourceExhausted || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED || !d.GetRetryable() ||
		d.GetRetryAfter().AsDuration() <= 0 {
		t.Fatalf("over the limit: %v / %v", code, d)
	}
	if e.rowCount(t, "quota_ledger", e.owner) != before || e.rowCount(t, "quota_reservations", e.owner) != 2 || len(e.provider.Calls()) != 2 {
		t.Fatal("a rate-limited call reserved something or reached the provider")
	}
	// The wait is real: after it the next call goes through.
	e.clock.Advance(d.GetRetryAfter().AsDuration())
	e.provider.Push(gwAnswer(t, gwOutput, 1, 1))
	e.mustComplete(t, gwSpec("c"))
}

// TestGatewayQuotaExceededAgainstPostgres: below the most the call may cost, nothing is reserved or called.
func TestGatewayQuotaExceededAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{limit: gwAmount() - 1}, gwAnswer(t, gwOutput, 1, 1))
	_, err := e.complete(t.Context(), gwSpec("job-1"))
	code, d := gwDetail(t, err)
	if code != connectrpc.CodeResourceExhausted || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED || d.GetRetryable() ||
		d.GetQuota().GetRemaining().GetValue() != gwAmount()-1 || d.GetQuota().GetLimit().GetValue() != gwAmount()-1 {
		t.Fatalf("quota exceeded: %v / %v", code, d)
	}
	if len(e.provider.Calls()) != 0 || e.rowCount(t, "quota_reservations", e.owner) != 0 || e.rowCount(t, "quota_idempotency_keys", e.owner) != 0 {
		t.Fatal("QUOTA_EXCEEDED reached the provider or stored something")
	}
}

func TestGatewayInvalidRequestsAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{})
	for name, mutate := range map[string]func(*hubv1.CompletionSpec){
		"unknown alias":      func(s *hubv1.CompletionSpec) { s.ModelAlias = "gpt" },
		"unspecified op":     func(s *hubv1.CompletionSpec) { s.OperationType = hubv1.OperationType_OPERATION_TYPE_UNSPECIFIED },
		"empty instructions": func(s *hubv1.CompletionSpec) { s.Instructions = "" },
		"key with space":     func(s *hubv1.CompletionSpec) { s.IdempotencyKey = "a b" },
		"over 1 MiB":         func(s *hubv1.CompletionSpec) { s.UntrustedData = strings.Repeat("d", values.MaxPayloadBytes) },
	} {
		spec := gwSpec("job-1")
		mutate(spec)
		if _, err := e.complete(t.Context(), spec); connectrpc.CodeOf(err) != connectrpc.CodeInvalidArgument {
			t.Errorf("%s: err = %v, want invalid_argument", name, err)
		}
	}
	if len(e.provider.Calls()) != 0 || e.rowCount(t, "quota_reservations", e.owner) != 0 {
		t.Fatal("an invalid request reached the provider or the ledger")
	}
}

// TestGatewayStoredResultEndsWithTheRetentionAgainstPostgres: after the retention the stored answer is never returned
// and the purge clears it from the database.
func TestGatewayStoredResultEndsWithTheRetentionAgainstPostgres(t *testing.T) {
	e := newGatewayEnv(t, gatewayOptions{}, gwAnswer(t, gwOutput, 1, 1), gwAnswer(t, "a fresh answer", 1, 1))
	e.mustComplete(t, gwSpec("job-1"))

	e.clock.Advance(gwRetention)
	if n, err := e.svc.PurgeResults(t.Context()); err != nil || n != 1 {
		t.Fatalf("PurgeResults = %d, %v; want 1", n, err)
	}
	var stored int
	if err := e.conn.QueryRow(t.Context(), "SELECT count(*) FROM quota_idempotency_keys WHERE result_output IS NOT NULL").Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("%d stored results after the purge (%v)", stored, err)
	}
	res := e.mustComplete(t, gwSpec("job-1"))
	if res.GetReplayed() || res.GetOutput() != "a fresh answer" || len(e.provider.Calls()) != 2 {
		t.Fatalf("after retention = %v", res)
	}
}
