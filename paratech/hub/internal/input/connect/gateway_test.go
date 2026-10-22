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
		// The agent operations are not served by Complete yet (TASK-114): the domain rejects "".
		hubv1.OperationType_OPERATION_TYPE_AGENT_STEP:        "",
		hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL:  "",
		hubv1.OperationType_OPERATION_TYPE_ANSWER:            "",
		hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT: "",
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
		{name: "conflict", err: fmt.Errorf("x: %w", gateway.ErrIdempotencyConflict), code: connectrpc.CodeFailedPrecondition,
			detail: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT},
		{name: "running", err: fmt.Errorf("x: %w", gateway.ErrAttemptRunning), code: connectrpc.CodeAborted},
		{name: "invalid", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrInvalidSpec), code: connectrpc.CodeInvalidArgument},
		{name: "too large", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, values.ErrPayloadTooLarge), code: connectrpc.CodeInvalidArgument},
		{name: "unknown alias", err: fmt.Errorf("%w: %w", gateway.ErrInvalidRequest, modelallowlist.ErrModelNotAllowed), code: connectrpc.CodeInvalidArgument},
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

func TestCompleteStreamIsUnimplemented(t *testing.T) {
	srv := gatewayServer(t, &fakeCompleter{}, nil)
	stream, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL).CompleteStream(t.Context(),
		withKey(connectrpc.NewRequest(&hubv1.CompleteStreamRequest{}), anyKey))
	if err == nil {
		for stream.Receive() {
		}
		err = stream.Err()
		_ = stream.Close()
	}
	if connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("CompleteStream error = %v, want unimplemented", err)
	}
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

// TestGatewayExposure pins ADR-0005(f) with the real handler mounted: Complete needs an API key, CompleteStream is
// unimplemented, the internal quota RPCs are not routed.
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
	for _, p := range []string{hubv1connect.QuotaServiceReserveProcedure, hubv1connect.QuotaServiceSettleProcedure, hubv1connect.QuotaServiceReleaseProcedure} {
		if code := postJSON(t, srv.URL+p, "{}", "Bearer "+anyKey); code != http.StatusNotFound {
			t.Errorf("%s: HTTP status = %d, want 404", p, code)
		}
	}
}
