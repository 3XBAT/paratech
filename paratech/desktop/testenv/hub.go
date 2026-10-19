package testenv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/domain/coursework/values"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// FakeHub is a local LLM Hub gateway for end-to-end tests of the desktop Hub client: it speaks the real Connect
// contract over TLS, answers each operation with a scripted model output and, like the real Hub, replays the stored
// result for an idempotency key it has seen (ADR-0005(d)), so a retry is charged once. A key reused with a different
// spec is answered with ERROR_CODE_IDEMPOTENCY_CONFLICT, as the real Hub does. It checks nothing else of the Hub's
// rules; the Hub has its own tests. It also serves the public QuotaService.GetRemaining with a scripted quota.
type FakeHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	hubv1connect.UnimplementedQuotaServiceHandler

	mu        sync.Mutex
	outputs   map[hubv1.OperationType]string
	results   map[string]*hubv1.CompletionResult // by idempotency key
	prints    map[string]string                  // fingerprint of the spec, by idempotency key
	requests  []*hubv1.CompleteRequest
	auth      []string
	quota     *hubv1.QuotaSnapshot // nil: GetRemaining answers unavailable
	quotaAuth []string
	rejectKey bool
	quotaDown bool
}

// NewFakeHub returns a Hub without scripted outputs; an operation without one is answered with unavailable.
func NewFakeHub() *FakeHub {
	return &FakeHub{
		outputs: map[hubv1.OperationType]string{}, results: map[string]*hubv1.CompletionResult{}, prints: map[string]string{},
	}
}

// Answer sets the model output of operation for keys the Hub has not seen yet.
func (h *FakeHub) Answer(op values.Operation, output string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs[hubOperation(op)] = output
}

func hubOperation(op values.Operation) hubv1.OperationType {
	switch op {
	case values.OperationSummary:
		return hubv1.OperationType_OPERATION_TYPE_SUMMARY
	case values.OperationExtractHomework:
		return hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES
	case values.OperationHomeworkDraft:
		return hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT
	case values.OperationLectureText:
		return hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT
	}
	return hubv1.OperationType_OPERATION_TYPE_UNSPECIFIED
}

// Complete implements the gateway: it records the request and the Authorization header and answers.
func (h *FakeHub) Complete(_ context.Context, r *connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, r.Msg)
	h.auth = append(h.auth, r.Header().Get("Authorization"))

	spec := r.Msg.GetSpec()
	key := spec.GetIdempotencyKey()
	fp, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if old, ok := h.prints[key]; ok && old != string(fp) {
		return nil, idempotencyConflict()
	}
	h.prints[key] = string(fp)
	if res, ok := h.results[key]; ok {
		return connect.NewResponse(&hubv1.CompleteResponse{Result: res}), nil
	}
	out, ok := h.outputs[spec.GetOperationType()]
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fake hub: no output scripted"))
	}
	res := &hubv1.CompletionResult{
		Output:        out,
		Usage:         &hubv1.Usage{InputTokens: int64(len(spec.GetUntrustedData())), OutputTokens: int64(len(out))},
		ReservationId: "res-" + key,
	}
	h.results[key] = res
	return connect.NewResponse(&hubv1.CompleteResponse{Result: res}), nil
}

// SetRemaining sets the quota GetRemaining reports, in tokens, for a quota that does not renew.
func (h *FakeHub) SetRemaining(remaining, reserved, limit int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tokens := func(v int64) *hubv1.QuotaAmount {
		return &hubv1.QuotaAmount{Unit: hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, Value: v}
	}
	h.quota = &hubv1.QuotaSnapshot{Remaining: tokens(remaining), Reserved: tokens(reserved), Limit: tokens(limit)}
}

// RejectKey makes GetRemaining answer INVALID_API_KEY, as the Hub does for a revoked key; false accepts keys again.
func (h *FakeHub) RejectKey(reject bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rejectKey = reject
}

// QuotaDown makes GetRemaining answer unavailable, as a Hub behind a failing proxy; false answers again.
func (h *FakeHub) QuotaDown(down bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.quotaDown = down
}

// GetRemaining implements the public quota method: it records the Authorization header and answers.
func (h *FakeHub) GetRemaining(_ context.Context, r *connect.Request[hubv1.GetRemainingRequest]) (*connect.Response[hubv1.GetRemainingResponse], error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.quotaAuth = append(h.quotaAuth, r.Header().Get("Authorization"))
	if h.rejectKey {
		e := connect.NewError(connect.CodeUnauthenticated, errors.New("fake hub: api key revoked"))
		d, err := connect.NewErrorDetail(&hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		e.AddDetail(d)
		return nil, e
	}
	if h.quota == nil || h.quotaDown {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fake hub: quota unavailable"))
	}
	return connect.NewResponse(&hubv1.GetRemainingResponse{Quota: proto.CloneOf(h.quota)}), nil
}

// QuotaAuthorizations returns the Authorization headers of the GetRemaining calls, in order.
func (h *FakeHub) QuotaAuthorizations() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.quotaAuth...)
}

// idempotencyConflict is the Hub's answer to a key reused with another spec (errors.proto).
func idempotencyConflict() error {
	e := connect.NewError(connect.CodeFailedPrecondition, errors.New("fake hub: idempotency key reused with another request"))
	d, err := connect.NewErrorDetail(&hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT})
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	e.AddDetail(d)
	return e
}

// Requests returns the requests that reached the Hub, in order.
func (h *FakeHub) Requests() []*hubv1.CompleteRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*hubv1.CompleteRequest(nil), h.requests...)
}

// Authorizations returns the Authorization headers of the requests, in order.
func (h *FakeHub) Authorizations() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.auth...)
}

// Charged returns the number of distinct idempotency keys the Hub executed: what a tester would pay for.
func (h *FakeHub) Charged() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.results)
}

// StartFakeHub serves h over TLS on a loopback port until the test ends and returns its base URL and an HTTP client
// that trusts its certificate.
func StartFakeHub(t testing.TB, h *FakeHub) (baseURL string, client *http.Client) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(h))
	mux.Handle(hubv1connect.NewQuotaServiceHandler(h))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, srv.Client()
}
