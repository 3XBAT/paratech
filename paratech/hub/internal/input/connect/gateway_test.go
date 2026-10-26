package connect_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeCompleter answers with res or err and remembers the commands.
type fakeCompleter struct {
	mu   sync.Mutex
	res  gateway.Result
	err  error
	cmds []gateway.Command
}

func (f *fakeCompleter) Complete(_ context.Context, cmd gateway.Command) (gateway.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	return f.res, f.err
}

func gatewayServer(t *testing.T, svc connect.Completer, log *slog.Logger) *httptest.Server {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	g, err := connect.NewGateway(svc, log)
	if err != nil {
		t.Fatal(err)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Gateway: g})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func completeRequest() *connectrpc.Request[hubv1.CompleteRequest] {
	return withKey(connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: "job-1", OperationType: hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES, ModelAlias: "default",
		Instructions: "Extract.", UntrustedData: "lesson", MaxOutputTokens: 77,
	}}), anyKey)
}

func TestNewGatewayRequiresDeps(t *testing.T) {
	if _, err := connect.NewGateway(nil, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("no error without a service")
	}
	if _, err := connect.NewGateway(&fakeCompleter{}, nil); err == nil {
		t.Error("no error without a logger")
	}
}

func TestCompleteMapsTheRequestAndTheResult(t *testing.T) {
	svc := &fakeCompleter{res: gateway.Result{
		Output: "answer \xff", InputTokens: 10, OutputTokens: 5, ReservationID: strings.Repeat("ab", 16), Replayed: true,
		Quota: gateway.Snapshot{Unit: "tokens", Remaining: 900, Reserved: 0, Limit: 1000},
	}}
	srv := gatewayServer(t, svc, nil)
	for codec, opts := range codecs {
		resp, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...).Complete(t.Context(), completeRequest())
		if err != nil {
			t.Fatalf("%s: %v", codec, err)
		}
		r := resp.Msg.GetResult()
		if r.GetOutput() != "answer �" || r.GetUsage().GetInputTokens() != 10 || r.GetUsage().GetOutputTokens() != 5 ||
			r.GetReservationId() != strings.Repeat("ab", 16) || !r.GetReplayed() || r.GetQuota().GetRemaining().GetValue() != 900 ||
			r.GetQuota().GetLimit().GetUnit() != hubv1.QuotaUnit_QUOTA_UNIT_TOKENS {
			t.Fatalf("%s: result = %v", codec, r)
		}
	}
	want := gateway.Command{
		Owner: "00000000000000aa", IdempotencyKey: "job-1", Operation: "extract_homework_deadlines", ModelAlias: "default",
		Instructions: "Extract.", UntrustedData: "lesson", MaxOutputTokens: 77,
	}
	if len(svc.cmds) != len(codecs) || svc.cmds[0] != want {
		t.Fatalf("commands = %+v, want %+v", svc.cmds, want)
	}
}

func TestCompleteMapsOperations(t *testing.T) {
	svc := &fakeCompleter{err: gateway.ErrInvalidRequest}
	srv := gatewayServer(t, svc, nil)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	for op, want := range map[hubv1.OperationType]string{
		hubv1.OperationType_OPERATION_TYPE_SUMMARY:                    "summary",
		hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES: "extract_homework_deadlines",
		hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT:             "homework_draft",
		hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT:               "lecture_text",
		// The readers run through Complete with a server prompt (ADR-0023); AGENT_STEP has its own RPC: the domain
		// rejects "".
		hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL:  "inspect_material",
		hubv1.OperationType_OPERATION_TYPE_ANSWER:            "answer",
		hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT: "triage_assignment",
		hubv1.OperationType_OPERATION_TYPE_AGENT_STEP:        "",
		hubv1.OperationType_OPERATION_TYPE_UNSPECIFIED:       "",
		hubv1.OperationType(99):                              "",
	} {
		req := completeRequest()
		req.Msg.Spec.OperationType = op
		_, _ = client.Complete(t.Context(), req)
		if got := svc.cmds[len(svc.cmds)-1].Operation; got != want {
			t.Errorf("%v: operation %q, want %q", op, got, want)
		}
	}
}

// detailOf returns the code and the single ErrorDetail of err (nil when there is none).
func detailOf(t *testing.T, err error) (connectrpc.Code, *hubv1.ErrorDetail, string) {
	t.Helper()
	var ce *connectrpc.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T (%v), want *connect.Error", err, err)
	}
	if len(ce.Details()) == 0 {
		return ce.Code(), nil, ce.Message()
	}
	if len(ce.Details()) != 1 {
		t.Fatalf("%d details, want 1", len(ce.Details()))
	}
	v, err := ce.Details()[0].Value()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := v.(*hubv1.ErrorDetail)
	if !ok {
		t.Fatalf("detail is %T", v)
	}
	return ce.Code(), d, ce.Message()
}

func TestCompleteErrorMapping(t *testing.T) {
	quota := gateway.Snapshot{Unit: "tokens", Remaining: 5, Reserved: 10, Limit: 100}
	transient := &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded", Status: 529}
	permanent := &values.ProviderError{Kind: values.KindPermanent, Reason: "rejected", Status: 401}
	tests := []struct {
		name      string
		err       error
		code      connectrpc.Code
		detail    hubv1.ErrorCode // UNSPECIFIED: no detail
		retryable bool
		check     func(*hubv1.ErrorDetail) bool
	}{
		{name: "rate limited", err: &gateway.RateLimitedError{RetryAfter: 3 * time.Second}, code: connectrpc.CodeResourceExhausted,
			detail: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, retryable: true,
			check: func(d *hubv1.ErrorDetail) bool { return d.GetRetryAfter().AsDuration() == 3*time.Second }},
		{name: "rate limited without a wait", err: &gateway.RateLimitedError{}, code: connectrpc.CodeResourceExhausted,
			detail: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, retryable: true,
			check: func(d *hubv1.ErrorDetail) bool { return d.GetRetryAfter().AsDuration() > 0 }},
		{name: "quota exceeded", err: fmt.Errorf("wrapped: %w", &gateway.QuotaExceededError{Quota: quota}), code: connectrpc.CodeResourceExhausted,
			detail: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED,
			check: func(d *hubv1.ErrorDetail) bool {
				return d.GetQuota().GetRemaining().GetValue() == 5 && d.GetQuota().GetReserved().GetValue() == 10 && d.GetRetryAfter() == nil
			}},
		{name: "transient provider error", err: &gateway.ProviderFailedError{Transient: true, RetryAfter: 9 * time.Second, Cause: transient},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, retryable: true,
			check: func(d *hubv1.ErrorDetail) bool { return d.GetRetryAfter().AsDuration() == 9*time.Second }},
		{name: "provider 401 is not an API key problem", err: &gateway.ProviderFailedError{Cause: permanent},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR,
			check: func(d *hubv1.ErrorDetail) bool { return d.GetRetryAfter() == nil }},
		// TASK-200: a refused reader answer says the model answered, not that the provider failed.
		{name: "invalid answer", err: &gateway.ProviderFailedError{Cause: fmt.Errorf("%w: %w", gateway.ErrInvalidAnswer, permanent)},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR,
			check: func(d *hubv1.ErrorDetail) bool { return strings.Contains(d.GetMessage(), "does not match the schema") }},
		{name: "conflict", err: fmt.Errorf("x: %w", gateway.ErrIdempotencyConflict), code: connectrpc.CodeFailedPrecondition,
			detail: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT},
		{name: "running", err: fmt.Errorf("x: %w", gateway.ErrAttemptRunning), code: connectrpc.CodeAborted},
		{name: "invalid", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrInvalidSpec), code: connectrpc.CodeInvalidArgument},
		{name: "too large", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrPayloadTooLarge), code: connectrpc.CodeInvalidArgument},
		{name: "unknown alias", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, modelallowlist.ErrModelNotAllowed), code: connectrpc.CodeInvalidArgument},
		{name: "client instructions", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrClientInstructions), code: connectrpc.CodeInvalidArgument},
		{name: "other alias", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrAliasMismatch), code: connectrpc.CodeInvalidArgument},
		{name: "unavailable operation", err: gateway.ErrOperationUnavailable, code: connectrpc.CodeInvalidArgument},
		{name: "hub failure", err: errors.New("db down at db.internal:5432"), code: connectrpc.CodeInternal},
		{name: "settlement failure", err: fmt.Errorf("settle: %w", account.ErrInvalidRecord), code: connectrpc.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged bytes.Buffer
			srv := gatewayServer(t, &fakeCompleter{err: tt.err}, slog.New(slog.NewTextHandler(&logged, nil)))
			for codec, opts := range codecs {
				_, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...).Complete(t.Context(), completeRequest())
				code, d, msg := detailOf(t, err)
				if code != tt.code {
					t.Fatalf("%s: code = %v, want %v", codec, code, tt.code)
				}
				if tt.detail == hubv1.ErrorCode_ERROR_CODE_UNSPECIFIED {
					if d != nil {
						t.Fatalf("%s: unexpected detail %v", codec, d)
					}
				} else if d == nil || d.GetCode() != tt.detail || d.GetRetryable() != tt.retryable || d.GetMessage() == "" ||
					(tt.check != nil && !tt.check(d)) {
					t.Fatalf("%s: detail = %v", codec, d)
				}
				// The client never sees the cause: no provider reason, status, host or record details.
				for _, leak := range []string{"overloaded", "529", "401", "rejected", "db.internal", "invalid quota record", "wrapped"} {
					if strings.Contains(msg, leak) || strings.Contains(d.GetMessage(), leak) {
						t.Fatalf("%s: answer %q / %q leaks %q", codec, msg, d.GetMessage(), leak)
					}
				}
			}
			if tt.code == connectrpc.CodeInternal && !strings.Contains(logged.String(), "completion failed") {
				t.Fatalf("a Hub failure is not logged: %s", logged.String())
			}
		})
	}
}

// fakeStreamer sends chunks, then answers with res or err, and remembers the commands.
type fakeStreamer struct {
	chunks []string
	res    gateway.Result
	err    error

	mu   sync.Mutex
	cmds []gateway.Command
}

func (f *fakeStreamer) CompleteStream(_ context.Context, cmd gateway.Command, onDelta func(string)) (gateway.Result, error) {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	f.mu.Unlock()
	for _, c := range f.chunks {
		onDelta(c)
	}
	return f.res, f.err
}

// notifyingStreamer reports on done when the use case has returned, whatever happened to the client.
type notifyingStreamer struct {
	connect.Streamer
	done chan error
}

func (n notifyingStreamer) CompleteStream(ctx context.Context, cmd gateway.Command, onDelta func(string)) (gateway.Result, error) {
	res, err := n.Streamer.CompleteStream(ctx, cmd, onDelta)
	n.done <- err
	return res, err
}

func streamServer(t *testing.T, s connect.Streamer, log *slog.Logger) *httptest.Server {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	g, err := connect.NewGateway(&fakeCompleter{}, log, connect.WithCompleteStream(s))
	if err != nil {
		t.Fatal(err)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Gateway: g})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func streamRequest(key, data string) *connectrpc.Request[hubv1.CompleteStreamRequest] {
	return withKey(connectrpc.NewRequest(&hubv1.CompleteStreamRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: key, OperationType: hubv1.OperationType_OPERATION_TYPE_ANSWER, UntrustedData: data,
	}}), anyKey)
}

// readStream reads a whole stream: the deltas joined, how many there were, the result and the end-of-stream error. A
// delta after the result or a second result fails the test.
func readStream(t *testing.T, ctx context.Context, client hubv1connect.GatewayServiceClient,
	req *connectrpc.Request[hubv1.CompleteStreamRequest]) (string, int, *hubv1.CompletionResult, error) {
	t.Helper()
	stream, err := client.CompleteStream(ctx, req)
	if err != nil {
		return "", 0, nil, err
	}
	defer func() { _ = stream.Close() }()
	var (
		text   strings.Builder
		n      int
		result *hubv1.CompletionResult
	)
	for stream.Receive() {
		if result != nil {
			t.Fatal("a message after the result")
		}
		switch e := stream.Msg().GetEvent().(type) {
		case *hubv1.CompleteStreamResponse_Delta:
			text.WriteString(e.Delta)
			n++
		case *hubv1.CompleteStreamResponse_Result:
			result = e.Result
		default:
			t.Fatalf("event %T", e)
		}
	}
	return text.String(), n, result, stream.Err()
}

func TestCompleteStreamSendsDeltasThenTheResult(t *testing.T) {
	s := &fakeStreamer{chunks: []string{`{"cl`, "aims \xff", `"}`}, res: gateway.Result{
		Output: `{"claims"}`, InputTokens: 10, OutputTokens: 5, ReservationID: strings.Repeat("ab", 16),
		Quota: gateway.Snapshot{Unit: "tokens", Remaining: 900, Limit: 1000},
	}}
	srv := streamServer(t, s, nil)
	for codec, opts := range codecs {
		client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...)
		text, n, r, err := readStream(t, t.Context(), client, streamRequest("ans-1", "data"))
		if err != nil {
			t.Fatalf("%s: %v", codec, err)
		}
		// Chunks may be joined into fewer deltas, never reordered or changed beyond UTF-8 repair.
		if text != `{"claims �"}` || n < 1 || n > 3 {
			t.Fatalf("%s: deltas %q in %d messages", codec, text, n)
		}
		if r.GetOutput() != `{"claims"}` || r.GetUsage().GetInputTokens() != 10 || r.GetReservationId() != strings.Repeat("ab", 16) ||
			r.GetReplayed() || r.GetQuota().GetRemaining().GetValue() != 900 {
			t.Fatalf("%s: result = %v", codec, r)
		}
	}
	want := gateway.Command{Owner: "00000000000000aa", IdempotencyKey: "ans-1", Operation: "answer", UntrustedData: "data"}
	if len(s.cmds) != len(codecs) || s.cmds[0] != want {
		t.Fatalf("commands = %+v, want %+v", s.cmds, want)
	}
}

// TestCompleteStreamErrors: an error ends the stream without a result, with the code and detail of Complete.
func TestCompleteStreamErrors(t *testing.T) {
	for name, tt := range map[string]struct {
		err    error
		code   connectrpc.Code
		detail hubv1.ErrorCode
	}{
		"provider error after deltas": {err: &gateway.ProviderFailedError{Transient: true, Cause: &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded"}},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR},
		"invalid answer": {err: &gateway.ProviderFailedError{Cause: fmt.Errorf("%w: x", gateway.ErrInvalidAnswer)},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR},
		"not streamable":  {err: fmt.Errorf("x: %w", gateway.ErrNotStreamable), code: connectrpc.CodeInvalidArgument},
		"rate limited":    {err: &gateway.RateLimitedError{RetryAfter: time.Second}, code: connectrpc.CodeResourceExhausted, detail: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED},
		"conflict":        {err: gateway.ErrIdempotencyConflict, code: connectrpc.CodeFailedPrecondition, detail: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT},
		"hub failure":     {err: errors.New("db down at db.internal:5432"), code: connectrpc.CodeInternal},
		"invalid request": {err: gateway.ErrInvalidRequest, code: connectrpc.CodeInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			srv := streamServer(t, &fakeStreamer{chunks: []string{"draft ", "text"}, err: tt.err}, nil)
			_, _, r, err := readStream(t, t.Context(), hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL), streamRequest("k", "d"))
			if r != nil {
				t.Fatalf("a result after the error: %v", r)
			}
			code, d, msg := detailOf(t, err)
			if code != tt.code || (tt.detail != hubv1.ErrorCode_ERROR_CODE_UNSPECIFIED && d.GetCode() != tt.detail) {
				t.Fatalf("code %v detail %v, want %v %v", code, d, tt.code, tt.detail)
			}
			for _, leak := range []string{"overloaded", "db.internal", "draft"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("message %q leaks %q", msg, leak)
				}
			}
		})
	}
}

const (
	streamPrompt   = "Answer from the fragments only."
	streamFragment = "Теорема Ролля: производная обращается в ноль."
	streamInput    = `{"question":"Что говорит теорема Ролля?","fragments":[{"chunk_id":"rec-1:t00:12:00","text":"` + streamFragment + `"}]}`
	streamAnswer   = `{"claims":[{"text":"Производная обращается в ноль.","cites":[{"chunk_id":"rec-1:t00:12:00",` +
		`"quote":"производная обращается в ноль"}]}],"missing":[]}`
)

// streamChunks is streamAnswer as the model writes it, fenced, in three chunks.
var streamChunks = []string{"```json\n" + streamAnswer[:20], streamAnswer[20:60], streamAnswer[60:] + "\n```"}

// answerService is the real gateway use case over the testenv fakes with the ANSWER server prompt.
func answerService(t *testing.T, providerStream bool, log *slog.Logger, steps ...testenv.ProviderStep) (*gateway.Service, *testenv.QuotaStore, *testenv.FakeProvider) {
	t.Helper()
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "strong", Model: "claude-strong-model", MaxOutput: 2000}})
	if err != nil {
		t.Fatal(err)
	}
	store, provider := testenv.NewQuotaStore(), testenv.NewFakeProvider(steps...)
	svc, err := gateway.New(gateway.Deps{
		Accounts: store, Results: store, Provider: provider, Streamer: provider, Limiter: &testenv.FakeLimiter{},
		Clock: testenv.NewFakeClock(time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)), Random: testenv.Random{}, Models: models, Log: log,
	}, gateway.Config{
		Unit: "tokens", Limit: 100_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: 5 * time.Minute,
		ServerPrompts:  map[values.Operation]gateway.ServerPrompt{values.OperationAnswer: {Alias: "strong", Version: "v1", Text: streamPrompt}},
		ProviderStream: providerStream,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, provider
}

func completionStep(t *testing.T, output string, chunks []string) testenv.ProviderStep {
	t.Helper()
	u, err := values.NewUsage(900, 120)
	if err != nil {
		t.Fatal(err)
	}
	c, err := values.NewCompletion(output, u, values.StopEnd)
	if err != nil {
		t.Fatal(err)
	}
	return testenv.ProviderStep{Completion: c, Chunks: chunks}
}

// TestCompleteStreamWithTheGatewayService runs the real use case behind the handler: deltas, the canonical result,
// replays across Complete and CompleteStream, and other operations refused.
func TestCompleteStreamWithTheGatewayService(t *testing.T) {
	for _, on := range []bool{false, true} {
		var logged bytes.Buffer
		log := slog.New(slog.NewTextHandler(&syncBuffer{w: &logged}, nil))
		svc, store, provider := answerService(t, on, log, completionStep(t, strings.Join(streamChunks, ""), streamChunks))
		g, err := connect.NewGateway(svc, log, connect.WithCompleteStream(svc))
		if err != nil {
			t.Fatal(err)
		}
		h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Gateway: g})
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

		text, _, r, err := readStream(t, t.Context(), client, streamRequest("ans-1", streamInput))
		if err != nil || r.GetOutput() != streamAnswer || r.GetReplayed() {
			t.Fatalf("provider_stream=%t: result %v, err %v", on, r, err)
		}
		wantText := streamAnswer // off: the checked output as one delta
		if on {
			wantText = strings.Join(streamChunks, "") // on: the model's raw text as it was written
		}
		if text != wantText {
			t.Fatalf("provider_stream=%t: deltas %q, want %q", on, text, wantText)
		}

		res, err := client.Complete(t.Context(), withKey(connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: streamRequest("ans-1", streamInput).Msg.GetSpec()}), anyKey))
		if err != nil || !res.Msg.GetResult().GetReplayed() || res.Msg.GetResult().GetOutput() != streamAnswer {
			t.Fatalf("provider_stream=%t: Complete replay %v, %v", on, res, err)
		}
		text, n, r, err := readStream(t, t.Context(), client, streamRequest("ans-1", streamInput))
		if err != nil || !r.GetReplayed() || text != streamAnswer || n != 1 {
			t.Fatalf("provider_stream=%t: stream replay %q in %d deltas, %v, %v", on, text, n, r, err)
		}
		if len(provider.Calls()) != 1 || (provider.Streams() == 1) != on {
			t.Fatalf("provider_stream=%t: calls %d, streams %d", on, len(provider.Calls()), provider.Streams())
		}
		if used, held := store.Totals("00000000000000aa"); used != 1020 || held != 0 {
			t.Fatalf("provider_stream=%t: used %d held %d", on, used, held)
		}

		req := streamRequest("sum-1", "lesson")
		req.Msg.Spec.OperationType = hubv1.OperationType_OPERATION_TYPE_SUMMARY
		req.Msg.Spec.Instructions, req.Msg.Spec.ModelAlias = "Summarize.", "strong"
		if _, _, _, err := readStream(t, t.Context(), client, req); connectrpc.CodeOf(err) != connectrpc.CodeInvalidArgument {
			t.Fatalf("provider_stream=%t: SUMMARY over CompleteStream = %v, want invalid_argument", on, err)
		}
		for _, text := range []string{"Ролля", "обращается", streamPrompt} {
			if strings.Contains(logged.String(), text) {
				t.Fatalf("provider_stream=%t: the log holds %q", on, text)
			}
		}
	}
}

// TestCompleteStreamClientCancelsMidStream: the client cancels after the first delta; the Hub reads the provider to the
// end, settles once in full and stores the answer; the next call with the key replays it free.
func TestCompleteStreamClientCancelsMidStream(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := completionStep(t, strings.Join(streamChunks, ""), streamChunks[:1])
	step.Wait, step.Entered = wait, entered
	svc, store, provider := answerService(t, true, slog.New(slog.DiscardHandler), step)
	done := make(chan error, 4)
	srv := streamServer(t, notifyingStreamer{Streamer: svc, done: done}, nil)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.CompleteStream(ctx, streamRequest("ans-1", streamInput))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() || stream.Msg().GetDelta() != streamChunks[0] {
		t.Fatalf("first message %v, err %v; want the first chunk", stream.Msg(), stream.Err())
	}
	<-entered
	cancel()
	_ = stream.Close()
	close(wait)
	if err := <-done; err != nil {
		t.Fatalf("the use case ended with %v after the cancel, want the call finished", err)
	}
	if used, held := store.Totals("00000000000000aa"); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d, want one full settlement", used, held)
	}

	text, n, r, err := readStream(t, t.Context(), client, streamRequest("ans-1", streamInput))
	if err != nil || !r.GetReplayed() || r.GetOutput() != streamAnswer || text != streamAnswer || n != 1 {
		t.Fatalf("retry %q in %d deltas, result %v, err %v", text, n, r, err)
	}
	if len(provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(provider.Calls()))
	}
	if used, _ := store.Totals("00000000000000aa"); used != 1020 {
		t.Fatalf("the replay was charged: used %d", used)
	}
}

// syncBuffer lets the race detector see that log writes and the final read do not overlap.
type syncBuffer struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func TestAgentStepIsUnimplemented(t *testing.T) {
	srv := gatewayServer(t, &fakeCompleter{}, nil)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

	_, err := client.AgentStep(t.Context(), withKey(connectrpc.NewRequest(&hubv1.AgentStepRequest{}), anyKey))
	if connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("AgentStep error = %v, want unimplemented", err)
	}
	if _, err := client.AgentStep(t.Context(), connectrpc.NewRequest(&hubv1.AgentStepRequest{})); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("AgentStep without a key = %v, want unauthenticated", err)
	}
}

// TestGatewayExposure pins ADR-0005(f) with the real handler mounted: Complete and CompleteStream need an API key,
// CompleteStream without a streamer is unimplemented, the internal quota RPCs are not routed.
func TestGatewayExposure(t *testing.T) {
	svc := &fakeCompleter{res: gateway.Result{Output: "ok", Quota: gateway.Snapshot{Unit: "tokens"}}}
	srv := gatewayServer(t, svc, nil)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

	req := completeRequest()
	req.Header().Del("Authorization")
	if _, err := client.Complete(t.Context(), req); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("Complete without a key: %v, want unauthenticated", err)
	}
	if len(svc.cmds) != 0 {
		t.Fatal("an unauthenticated call reached the use case")
	}
	if _, err := client.Complete(t.Context(), completeRequest()); err != nil {
		t.Fatalf("Complete with a key: %v", err)
	}
	if _, _, _, err := readStream(t, t.Context(), client, streamRequest("k", "d")); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("CompleteStream without a streamer: %v, want unimplemented", err)
	}

	s := &fakeStreamer{res: gateway.Result{Quota: gateway.Snapshot{Unit: "tokens"}}}
	streaming := hubv1connect.NewGatewayServiceClient(srv.Client(), streamServer(t, s, nil).URL)
	unauthenticated := streamRequest("k", "d")
	unauthenticated.Header().Del("Authorization")
	if _, _, _, err := readStream(t, t.Context(), streaming, unauthenticated); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("CompleteStream without a key: %v, want unauthenticated", err)
	}
	if len(s.cmds) != 0 {
		t.Fatal("an unauthenticated stream reached the use case")
	}
	for _, p := range []string{hubv1connect.QuotaServiceReserveProcedure, hubv1connect.QuotaServiceSettleProcedure, hubv1connect.QuotaServiceReleaseProcedure} {
		if code := postJSON(t, srv.URL+p, "{}", "Bearer "+anyKey); code != http.StatusNotFound {
			t.Errorf("%s: HTTP status = %d, want 404", p, code)
		}
	}
}
