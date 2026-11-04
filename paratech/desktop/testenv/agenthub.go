package testenv

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

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
// answers unavailable, as when the connection drops after the Hub finished. deltas are the reply texts AgentStepStream
// sends before the result or the error; held keeps the stream open after them until the client goes away (the answer
// is stored for its key all the same, as the real Hub finishes a step whose client left).
type agentAnswer struct {
	calls  []AgentCall
	err    func() error
	lost   bool
	deltas []string
	held   bool
}

// maxHold bounds a held stream, so a client that never goes away cannot keep the test server from closing.
const maxHold = 10 * time.Second

// FakeAgentHub is a local Hub gateway for the agent planner (AgentStep and AgentStepStream) in end-to-end tests. It
// speaks the real Connect contract over TLS, answers each new key with the next scripted step and, like the real Hub,
// replays what it stored for a key it has seen (an answer as Replayed, with no deltas; a failure as the same failure)
// through either RPC and answers a key reused with another request with ERROR_CODE_IDEMPOTENCY_CONFLICT. It checks the
// history shape the provider needs (the student's text first, every echo followed by the results of exactly its calls)
// and the seals of echoed calls. It checks nothing else of the Hub's rules; the Hub has its own tests.
type FakeAgentHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu       sync.Mutex
	script   []agentAnswer
	answers  map[string]*hubv1.AgentStepResponse
	failures map[string]func() error
	prints   map[string][]byte
	requests []*hubv1.AgentStepRequest
	catalog  uint32
	noStream bool          // AgentStepStream answers unimplemented, as an older Hub
	streams  int           // AgentStepStream calls served
	unary    int           // AgentStep calls served
	sent     chan struct{} // closed once the first held stream sent its deltas
	sentDone bool
}

// AsCatalog makes the Hub treat the next requests as sent by a desktop on catalog version v (one before an update):
// it seals and checks calls for v instead of the request's version. Zero restores the request's version.
func (h *FakeAgentHub) AsCatalog(v uint32) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.catalog = v
	return h
}

func (h *FakeAgentHub) catalogOf(req *hubv1.AgentStepRequest) uint32 {
	if h.catalog != 0 {
		return h.catalog
	}
	return req.GetCatalogVersion()
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

// ThenStream scripts the next step to return calls, AgentStepStream sending deltas (the reply as the model writes it)
// before the result; AgentStep returns the calls only.
func (h *FakeAgentHub) ThenStream(deltas []string, calls ...AgentCall) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{calls: calls, deltas: deltas})
	return h
}

// ThenStreamHeld is ThenStream whose stream stays open after the deltas until the client goes away (or maxHold): the
// Hub stores the answer for the key, so the same key replays it. Sent tells when the deltas went out.
func (h *FakeAgentHub) ThenStreamHeld(deltas []string, calls ...AgentCall) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{calls: calls, deltas: deltas, held: true})
	return h
}

// ThenStreamFails scripts the next step to send deltas, then fail as ThenProviderFails does.
func (h *FakeAgentHub) ThenStreamFails(deltas ...string) *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.script = append(h.script, agentAnswer{err: providerFailed, deltas: deltas})
	return h
}

// WithoutStream makes AgentStepStream answer unimplemented, as a Hub older than TASK-340.
func (h *FakeAgentHub) WithoutStream() *FakeAgentHub {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.noStream = true
	return h
}

// Calls returns how many AgentStepStream and AgentStep calls the Hub served.
func (h *FakeAgentHub) Calls() (streams, unary int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.streams, h.unary
}

// Sent is closed once the first held stream sent its deltas.
func (h *FakeAgentHub) Sent() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sent == nil {
		h.sent = make(chan struct{})
	}
	return h.sent
}

// ThenProviderFails scripts a provider failure that is not retryable; the Hub keeps it for the key.
func (h *FakeAgentHub) ThenProviderFails() *FakeAgentHub {
	return h.thenErr(providerFailed)
}

func providerFailed() error {
	return detailed(connect.CodeUnavailable, &hubv1.ErrorDetail{
		Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "fake hub: provider failed",
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
	h.unary++
	h.mu.Unlock()
	out := h.step(r.Msg)
	if out.err != nil {
		return nil, out.err
	}
	return connect.NewResponse(out.resp), nil
}

// AgentStepStream implements the gateway: the deltas of a fresh step, then its result; a replay sends only the result.
func (h *FakeAgentHub) AgentStepStream(ctx context.Context, r *connect.Request[hubv1.AgentStepStreamRequest],
	s *connect.ServerStream[hubv1.AgentStepStreamResponse],
) error {
	h.mu.Lock()
	if h.noStream {
		h.mu.Unlock()
		return connect.NewError(connect.CodeUnimplemented, errors.New("fake hub: AgentStepStream is not implemented"))
	}
	h.streams++
	h.mu.Unlock()
	if r.Msg.GetRequest() == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("fake hub: no request"))
	}

	out := h.step(r.Msg.GetRequest())
	for _, d := range out.deltas {
		if err := s.Send(&hubv1.AgentStepStreamResponse{Event: &hubv1.AgentStepStreamResponse_Delta{Delta: d}}); err != nil {
			return err
		}
	}
	if out.held {
		h.mu.Lock()
		if h.sent == nil {
			h.sent = make(chan struct{})
		}
		if !h.sentDone {
			close(h.sent) // the first held stream only
			h.sentDone = true
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(maxHold):
		}
	}
	if out.err != nil {
		return out.err
	}
	return s.Send(&hubv1.AgentStepStreamResponse{Event: &hubv1.AgentStepStreamResponse_Result{Result: out.resp}})
}

// stepOutcome is what one request gets: the response or the error, and for a fresh step its deltas.
type stepOutcome struct {
	resp   *hubv1.AgentStepResponse
	err    error
	deltas []string
	held   bool
}

// step runs the ledger of one request for either RPC.
func (h *FakeAgentHub) step(req *hubv1.AgentStepRequest) stepOutcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, req)
	key := req.GetIdempotencyKey()

	fp, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		return stepOutcome{err: connect.NewError(connect.CodeInternal, err)}
	}
	if prev, seen := h.prints[key]; seen {
		if string(prev) != string(fp) {
			return stepOutcome{err: idempotencyConflict()}
		}
		if fail, ok := h.failures[key]; ok {
			return stepOutcome{err: fail()}
		}
		if resp, ok := h.answers[key]; ok {
			replay := proto.Clone(resp).(*hubv1.AgentStepResponse)
			replay.Replayed = true
			return stepOutcome{resp: replay}
		}
	}
	if err := checkAgentHistory(req, h.catalogOf(req)); err != nil {
		return stepOutcome{err: connect.NewError(connect.CodeInvalidArgument, err)}
	}
	if len(h.script) == 0 {
		return stepOutcome{err: connect.NewError(connect.CodeUnavailable, errors.New("fake hub: no scripted step"))}
	}
	next := h.script[0]
	h.script = h.script[1:]
	h.prints[key] = fp
	if next.err != nil {
		h.failures[key] = next.err
		return stepOutcome{err: next.err(), deltas: next.deltas}
	}
	resp := &hubv1.AgentStepResponse{
		Calls: &hubv1.SealedCalls{}, Usage: &hubv1.Usage{InputTokens: 1200, OutputTokens: 60}, ReservationId: "res-" + key,
	}
	for _, c := range next.calls {
		resp.Calls.Calls = append(resp.Calls.Calls, &hubv1.ToolCall{CallId: c.ID, Tool: c.Tool, ArgumentsJson: c.Args})
	}
	resp.Calls.Seal = agentSeal(req.GetThreadId(), h.catalogOf(req), resp.Calls.Calls)
	h.answers[key] = resp
	out := stepOutcome{resp: resp, deltas: next.deltas, held: next.held}
	if next.lost {
		out.err = connect.NewError(connect.CodeUnavailable, errors.New("fake hub: connection reset"))
	}
	return out
}

// agentSeal stands for the Hub's HMAC: bound to the thread, the catalog version and the calls in order.
func agentSeal(thread string, catalog uint32, calls []*hubv1.ToolCall) []byte {
	s := sha256.New()
	s.Write([]byte(thread))
	s.Write([]byte{0})
	s.Write([]byte(strconv.FormatUint(uint64(catalog), 10)))
	for _, c := range calls {
		s.Write([]byte{0})
		s.Write([]byte(c.GetCallId() + "\x00" + c.GetTool() + "\x00" + c.GetArgumentsJson()))
	}
	return s.Sum(nil)
}

func checkAgentHistory(req *hubv1.AgentStepRequest, catalog uint32) error {
	turns := req.GetTurns()
	// From catalog 9 the history may open with the chat's summary, always followed by a student text (ADR-0028).
	if len(turns) > 0 && turns[0].GetHistorySummary() != "" && req.GetCatalogVersion() >= 9 {
		turns = turns[1:]
	}
	if len(turns) == 0 || turns[0].GetStudentText() == "" {
		return errors.New("fake hub: the first turn must be the student's text")
	}
	for i, t := range turns {
		switch {
		case t.GetCalls() != nil:
			calls := t.GetCalls()
			if string(calls.GetSeal()) != string(agentSeal(req.GetThreadId(), catalog, calls.GetCalls())) {
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

// Requests returns the step requests that reached the Hub through either RPC, in order.
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
