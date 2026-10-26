package hubclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeAgentHub serves AgentStep; answer runs on the server goroutine and must not call t.Fatal.
type fakeAgentHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu      sync.Mutex
	reqs    []*hubv1.AgentStepRequest
	headers []http.Header
	answer  func() (*hubv1.AgentStepResponse, error)
}

func (f *fakeAgentHub) AgentStep(_ context.Context, r *connect.Request[hubv1.AgentStepRequest]) (*connect.Response[hubv1.AgentStepResponse], error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Msg)
	f.headers = append(f.headers, r.Header().Clone())
	f.mu.Unlock()
	resp, err := f.answer()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (f *fakeAgentHub) seen() []*hubv1.AgentStepRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*hubv1.AgentStepRequest(nil), f.reqs...)
}

func startAgentHub(t *testing.T, f *fakeAgentHub) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 5 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func agentOK() (*hubv1.AgentStepResponse, error) {
	return &hubv1.AgentStepResponse{
		Calls: &hubv1.SealedCalls{
			Calls: []*hubv1.ToolCall{{CallId: "toolu_1", Tool: "list_subjects", ArgumentsJson: `{}`}},
			Seal:  []byte{1, 2, 3},
		},
		Usage: &hubv1.Usage{InputTokens: 900, OutputTokens: 40}, ReservationId: "res-1", Replayed: true,
	}, nil
}

func stepRequest(t *testing.T) agent.StepRequest {
	t.Helper()
	s1, err := agent.NewRef(agent.KindSubject, "s1")
	if err != nil {
		t.Fatal(err)
	}
	l1, _ := agent.NewRef(agent.KindLesson, "l1")
	today, _ := agent.NewDate(2026, 10, 22)
	base, err := agent.NewBase([]agent.BaseSubject{{ID: s1, Name: "Физика"}}, today, "Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := agent.NewScope(s1, agent.ScreenSchedule, l1, agent.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	return agent.StepRequest{
		Key: "ag:th1:1:2", Thread: "th1", Base: base, Scope: scope, MaxOutputTokens: 1024,
		History: []agent.HistoryTurn{
			{StudentText: "какие пары"},
			{Calls: &agent.SealedCalls{Calls: []agent.RawCall{{CallID: "toolu_0", Tool: "find_lessons", ArgumentsJSON: `{}`}}, Seal: []byte{9}}},
			{Results: []agent.ToolResult{{CallID: "toolu_0", ResultJSON: `{"n":2}`, IsError: true}}},
		},
	}
}

func TestStepSendsExactlyTheContractFields(t *testing.T) {
	f := &fakeAgentHub{answer: agentOK}
	c := startAgentHub(t, f)

	got, err := c.Step(t.Context(), stepRequest(t))
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !got.Replayed || got.InputTokens != 900 || got.OutputTokens != 40 || string(got.Calls.Seal) != "\x01\x02\x03" ||
		len(got.Calls.Calls) != 1 || got.Calls.Calls[0] != (agent.RawCall{CallID: "toolu_1", Tool: "list_subjects", ArgumentsJSON: `{}`}) {
		t.Fatalf("reply = %+v", got)
	}

	want := &hubv1.AgentStepRequest{
		IdempotencyKey: "ag:th1:1:2", ThreadId: "th1", Profile: hubv1.AgentProfile_AGENT_PROFILE_PANEL, CatalogVersion: agent.CatalogVersion,
		Base: &hubv1.AgentBase{
			Subjects: []*hubv1.AgentSubject{{SubjectId: "s1", Name: "Физика"}}, Today: "2026-10-22", TimeZone: "Europe/Moscow",
		},
		Scope: &hubv1.AgentScope{SubjectId: "s1", Screen: hubv1.AgentScreen_AGENT_SCREEN_SCHEDULE, LessonId: "l1"},
		Turns: []*hubv1.AgentTurn{
			{Turn: &hubv1.AgentTurn_StudentText{StudentText: "какие пары"}},
			{Turn: &hubv1.AgentTurn_Calls{Calls: &hubv1.SealedCalls{
				Calls: []*hubv1.ToolCall{{CallId: "toolu_0", Tool: "find_lessons", ArgumentsJson: `{}`}}, Seal: []byte{9},
			}}},
			{Turn: &hubv1.AgentTurn_Results{Results: &hubv1.ToolResults{
				Results: []*hubv1.ToolResult{{CallId: "toolu_0", ResultJson: `{"n":2}`, IsError: true}},
			}}},
		},
		MaxOutputTokens: 1024,
	}
	reqs := f.seen()
	if len(reqs) != 1 || !proto.Equal(reqs[0], want) {
		t.Fatalf("request on the wire:\n got %v\nwant %v", reqs, want)
	}
	if h := f.headers[0].Get("Authorization"); h != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", h)
	}
}

func TestStepErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		want   []error
		absent []error
	}{
		{
			name: "turn gone", err: connect.NewError(connect.CodeFailedPrecondition, errors.New("start again")),
			want: []error{ErrTurnGone, agent.ErrTurnGone, backoff.ErrPermanent},
		},
		{
			name: "idempotency conflict is not turn gone",
			err: detailErr(connect.CodeFailedPrecondition, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, Message: "conflict",
			}),
			want: []error{ErrIdempotencyConflict, backoff.ErrPermanent}, absent: []error{agent.ErrTurnGone},
		},
		{
			// Only a failed_precondition without any detail is turn gone (ADR-0023); a detail of any code is not.
			name: "failed precondition with another detail is not turn gone",
			err: detailErr(connect.CodeFailedPrecondition, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "other",
			}),
			absent: []error{ErrTurnGone, agent.ErrTurnGone},
		},
		{
			name: "quota",
			err:  detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: "q"}),
			want: []error{shared.ErrQuotaExceeded},
		},
		{
			name: "provider retryable",
			err: detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true, Message: "busy",
			}),
			want: []error{ErrProvider, backoff.ErrTransient, shared.ErrServiceUnavailable},
		},
		{
			name: "provider failed for good",
			err:  detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "x"}),
			want: []error{ErrProvider, backoff.ErrPermanent},
		},
		{name: "busy", err: connect.NewError(connect.CodeAborted, errors.New("running")), want: []error{ErrBusy, backoff.ErrTransient}},
		{name: "bad seal", err: connect.NewError(connect.CodeInvalidArgument, errors.New("seal")), want: []error{ErrRejected, backoff.ErrPermanent}},
		{name: "key", err: connect.NewError(connect.CodeUnauthenticated, errors.New("key")), want: []error{ErrInvalidAPIKey}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startAgentHub(t, &fakeAgentHub{answer: func() (*hubv1.AgentStepResponse, error) { return nil, tt.err }})
			_, err := c.Step(t.Context(), stepRequest(t))
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Errorf("error %v does not match %v", err, w)
				}
			}
			for _, a := range tt.absent {
				if errors.Is(err, a) {
					t.Errorf("error %v matches %v", err, a)
				}
			}
		})
	}
}

func TestStepBadResponses(t *testing.T) {
	bad := map[string]*hubv1.AgentStepResponse{
		"no calls":  {Calls: &hubv1.SealedCalls{Seal: []byte{1}}},
		"no seal":   {Calls: &hubv1.SealedCalls{Calls: []*hubv1.ToolCall{{CallId: "a", Tool: "reply"}}}},
		"dup calls": {Calls: &hubv1.SealedCalls{Calls: []*hubv1.ToolCall{{CallId: "a"}, {CallId: "a"}}, Seal: []byte{1}}},
		"bad id":    {Calls: &hubv1.SealedCalls{Calls: []*hubv1.ToolCall{{CallId: "a b"}}, Seal: []byte{1}}},
	}
	for name, resp := range bad {
		t.Run(name, func(t *testing.T) {
			c := startAgentHub(t, &fakeAgentHub{answer: func() (*hubv1.AgentStepResponse, error) { return resp, nil }})
			if _, err := c.Step(t.Context(), stepRequest(t)); !errors.Is(err, ErrBadResponse) || !errors.Is(err, backoff.ErrPermanent) {
				t.Fatalf("Step = %v, want a permanent ErrBadResponse", err)
			}
		})
	}
}

func TestStepRefusesBadRequestsLocally(t *testing.T) {
	f := &fakeAgentHub{answer: agentOK}
	c := startAgentHub(t, f)
	for name, mut := range map[string]func(r *agent.StepRequest){
		"no key":      func(r *agent.StepRequest) { r.Key = "" },
		"no thread":   func(r *agent.StepRequest) { r.Thread = "" },
		"no history":  func(r *agent.StepRequest) { r.History = nil },
		"bad entry":   func(r *agent.StepRequest) { r.History[0] = agent.HistoryTurn{} },
		"huge output": func(r *agent.StepRequest) { r.MaxOutputTokens = 1 << 30 },
		"bad result":  func(r *agent.StepRequest) { r.History[2].Results[0].ResultJSON = "{" },
	} {
		req := stepRequest(t)
		mut(&req)
		if _, err := c.Step(t.Context(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(f.seen()) != 0 {
		t.Fatal("a refused request reached the Hub")
	}
}

func TestStepNotConfigured(t *testing.T) {
	c, err := New(Config{BaseURL: "https://hub.invalid"}, testenv.NewMemCredentials())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(t.Context(), stepRequest(t)); !errors.Is(err, ErrNotConfigured) || !errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("Step without a key = %v", err)
	}
}
