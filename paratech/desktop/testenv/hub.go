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
// rules; the Hub has its own tests.
type FakeHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu       sync.Mutex
	outputs  map[hubv1.OperationType]string
	results  map[string]*hubv1.CompletionResult // by idempotency key
	prints   map[string]string                  // fingerprint of the spec, by idempotency key
	requests []*hubv1.CompleteRequest
	auth     []string
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
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, srv.Client()
}
