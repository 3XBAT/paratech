package testenv

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// AgentCall is one tool call a scripted planner step returns.
type AgentCall struct {
	ID   string
	Tool string
	Args string
}

// agentAnswer is one scripted step: calls, or the error the Hub answers with. lost stores the answer for its key but
// answers unavailable, as when the connection drops after the Hub finished.
type agentAnswer struct {
	calls []AgentCall
	err   func() error
	lost  bool
}

// FakeAgentHub is a local Hub gateway for the agent planner (AgentStep) in end-to-end tests. It speaks the real
// Connect contract over TLS, answers each new key with the next scripted step and, like the real Hub, replays what it
// stored for a key it has seen (an answer as Replayed, a failure as the same failure) and answers a key reused with
// another request with ERROR_CODE_IDEMPOTENCY_CONFLICT. It checks the history shape the provider needs (the student's
// text first, every echo followed by the results of exactly its calls) and the seals of echoed calls. It checks
// nothing else of the Hub's rules; the Hub has its own tests.
type FakeAgentHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu       sync.Mutex
	script   []agentAnswer
	answers  map[string]*hubv1.AgentStepResponse
	failures map[string]func() error
	prints   map[string][]byte
	requests []*hubv1.AgentStepRequest
}

// NewFakeAgentHub returns a Hub without scripted steps; a step without one is answered with unavailable.
func NewFakeAgentHub() *FakeAgentHub {
	return &FakeAgentHub{
		answers: map[string]*hubv1.AgentStepResponse{}, failures: map[string]func() error{}, prints: map[string][]byte{},
	}
}

// Then scripts the next step to return calls.
func (h *FakeAgentHub) Then(calls ...AgentCall) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{calls: calls})
	return h
}

// ThenLost scripts the next step to finish on the Hub while its answer is lost on the way (unavailable); the same
// key gets the stored answer for free.
func (h *FakeAgentHub) ThenLost(calls ...AgentCall) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{calls: calls, lost: true})
	return h
}

// ThenProviderFails scripts a provider failure that is not retryable; the Hub keeps it for the key.
func (h *FakeAgentHub) ThenProviderFails() *FakeAgentHub {
	return h.thenErr(func() error {
		return detailed(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "fake hub: provider failed",
		})
	})
}

// ThenTurnGone scripts the Hub's answer when it no longer keeps the model turn of the last echo.
func (h *FakeAgentHub) ThenTurnGone() *FakeAgentHub {
	return h.thenErr(func() error {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("fake hub: the model turn is gone"))
	})
}

func (h *FakeAgentHub) thenErr(err func() error) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{err: err})
	return h
}

func detailed(code connect.Code, d *hubv1.ErrorDetail) error {
	e := connect.NewError(code, errors.New(d.GetMessage()))
	detail, err := connect.NewErrorDetail(d)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	e.AddDetail(detail)
	return e
}

// AgentStep implements the gateway.
func (h *FakeAgentHub) AgentStep(_ context.Context, r *connect.Request[hubv1.AgentStepRequest]) (*connect.Response[hubv1.AgentStepResponse], error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	req := r.Msg
	h.requests = append(h.requests, req)
	key := req.GetIdempotencyKey()

	fp, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if prev, seen := h.prints[key]; seen {
		if string(prev) != string(fp) {
			return nil, idempotencyConflict()
		}
		if fail, ok := h.failures[key]; ok {
			return nil, fail()
		}
		if resp, ok := h.answers[key]; ok {
			replay := proto.Clone(resp).(*hubv1.AgentStepResponse)
			replay.Replayed = true
			return connect.NewResponse(replay), nil
		}
	}
	if err := checkAgentHistory(req); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if len(h.script) == 0 {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fake hub: no scripted step"))
	}
	next := h.script[0]
	h.script = h.script[1:]
	h.prints[key] = fp
	if next.err != nil {
		h.failures[key] = next.err
		return nil, next.err()
	}
	resp := &hubv1.AgentStepResponse{
		Calls: &hubv1.SealedCalls{}, Usage: &hubv1.Usage{InputTokens: 1200, OutputTokens: 60}, ReservationId: "res-" + key,
	}
	for _, c := range next.calls {
		resp.Calls.Calls = append(resp.Calls.Calls, &hubv1.ToolCall{CallId: c.ID, Tool: c.Tool, ArgumentsJson: c.Args})
	}
	resp.Calls.Seal = agentSeal(req.GetThreadId(), resp.Calls.Calls)
	h.answers[key] = resp
	if next.lost {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("fake hub: connection reset"))
	}
	return connect.NewResponse(resp), nil
}

// agentSeal stands for the Hub's HMAC: bound to the thread and the calls in order.
func agentSeal(thread string, calls []*hubv1.ToolCall) []byte {
	s := sha256.New()
	s.Write([]byte(thread))
	for _, c := range calls {
		s.Write([]byte{0})
		s.Write([]byte(c.GetCallId() + "\x00" + c.GetTool() + "\x00" + c.GetArgumentsJson()))
	}
	return s.Sum(nil)
}

func checkAgentHistory(req *hubv1.AgentStepRequest) error {
	turns := req.GetTurns()
	if len(turns) == 0 || turns[0].GetStudentText() == "" {
		return errors.New("fake hub: the first turn must be the student's text")
	}
	for i, t := range turns {
		switch {
		case t.GetCalls() != nil:
			calls := t.GetCalls()
			if string(calls.GetSeal()) != string(agentSeal(req.GetThreadId(), calls.GetCalls())) {
				return errors.New("fake hub: the seal of echoed calls does not match")
			}
			if i+1 >= len(turns) || turns[i+1].GetResults() == nil || !sameAgentCalls(calls.GetCalls(), turns[i+1].GetResults().GetResults()) {
				return errors.New("fake hub: every echo must be followed by the results of exactly its calls")
			}
		case t.GetResults() != nil:
			if i == 0 || turns[i-1].GetCalls() == nil {
				return errors.New("fake hub: results must follow an echo")
			}
		}
	}
	return nil
}

func sameAgentCalls(calls []*hubv1.ToolCall, results []*hubv1.ToolResult) bool {
	if len(calls) != len(results) {
		return false
	}
	ids := map[string]bool{}
	for _, c := range calls {
		ids[c.GetCallId()] = true
	}
	for _, r := range results {
		if !ids[r.GetCallId()] {
			return false
		}
	}
	return true
}

// Requests returns the AgentStep requests that reached the Hub, in order.
func (h *FakeAgentHub) Requests() []*hubv1.AgentStepRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*hubv1.AgentStepRequest(nil), h.requests...)
}

// Executed returns the number of distinct keys the Hub ran: what a tester would pay for.
func (h *FakeAgentHub) Executed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.prints)
}

// StartFakeAgentHub serves h over TLS on a loopback port until the test ends and returns its base URL and an HTTP
// client that trusts its certificate.
func StartFakeAgentHub(t testing.TB, h *FakeAgentHub) (baseURL string, client *http.Client) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(h))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, srv.Client()
}
