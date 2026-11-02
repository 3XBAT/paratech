package connect_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/domain/llm/services/modelallowlist"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/input/connect"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeAgentStreamer sends chunks, then answers with res or err, and remembers the commands.
type fakeAgentStreamer struct {
	chunks []string
	res    agentstep.Result
	err    error

	mu   sync.Mutex
	cmds []agentstep.Command
}

func (f *fakeAgentStreamer) StepStream(_ context.Context, cmd agentstep.Command, onDelta func(string)) (agentstep.Result, error) {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	f.mu.Unlock()
	for _, c := range f.chunks {
		onDelta(c)
	}
	return f.res, f.err
}

// notifyingAgentStreamer reports on done when the use case has returned, whatever happened to the client.
type notifyingAgentStreamer struct {
	connect.AgentStreamer
	done chan error
}

func (n notifyingAgentStreamer) StepStream(ctx context.Context, cmd agentstep.Command, onDelta func(string)) (agentstep.Result, error) {
	res, err := n.AgentStreamer.StepStream(ctx, cmd, onDelta)
	n.done <- err
	return res, err
}

func agentStreamServer(t *testing.T, s connect.AgentStreamer, stepper connect.AgentStepper) *httptest.Server {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	opts := []connect.GatewayOption{connect.WithAgentStepStream(s)}
	if stepper != nil {
		opts = append(opts, connect.WithAgentStep(stepper))
	}
	g, err := connect.NewGateway(&fakeCompleter{}, log, opts...)
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

func agentStreamRequest() *connectrpc.Request[hubv1.AgentStepStreamRequest] {
	inner := agentRequest()
	req := connectrpc.NewRequest(&hubv1.AgentStepStreamRequest{Request: inner.Msg})
	req.Header().Set("Authorization", inner.Header().Get("Authorization"))
	return req
}

// readAgentStream reads a whole AgentStepStream: the deltas, the result and the end-of-stream error. A message after the
// result fails the test, and so does a delta that is not valid UTF-8.
func readAgentStream(t *testing.T, ctx context.Context, client hubv1connect.GatewayServiceClient,
	req *connectrpc.Request[hubv1.AgentStepStreamRequest]) ([]string, *hubv1.AgentStepResponse, error) {
	t.Helper()
	stream, err := client.AgentStepStream(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = stream.Close() }()
	var (
		deltas []string
		result *hubv1.AgentStepResponse
	)
	for stream.Receive() {
		if result != nil {
			t.Fatal("a message after the result")
		}
		switch e := stream.Msg().GetEvent().(type) {
		case *hubv1.AgentStepStreamResponse_Delta:
			if !utf8.ValidString(e.Delta) || strings.ContainsRune(e.Delta, utf8.RuneError) {
				t.Fatalf("delta %q is cut inside a character", e.Delta)
			}
			deltas = append(deltas, e.Delta)
		case *hubv1.AgentStepStreamResponse_Result:
			result = e.Result
		default:
			t.Fatalf("event %T", e)
		}
	}
	return deltas, result, stream.Err()
}

// TestAgentStepStreamSendsDeltasThenTheResult: deltas cut only between characters, in order, then the result mapped
// as AgentStep maps it; the request maps to the command AgentStep builds.
func TestAgentStepStreamSendsDeltasThenTheResult(t *testing.T) {
	const ye = "е"
	res := agentstep.Result{
		Calls: []agentstep.Call{{ID: "toolu_2", Tool: "reply", Arguments: `{"text":"Готово, всё."}`}}, Seal: []byte{1, 9, 9},
		InputTokens: 40, OutputTokens: 7, ReservationID: strings.Repeat("cd", 16),
		Quota: agentstep.Snapshot{Unit: "tokens", Remaining: 50, Limit: 100},
	}
	s := &fakeAgentStreamer{chunks: []string{"Готово, вс" + ye[:1], ye[1:] + "."}, res: res}
	stepper := &fakeStepper{res: res}
	srv := agentStreamServer(t, s, stepper)
	for codec, opts := range codecs {
		client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...)
		deltas, r, err := readAgentStream(t, t.Context(), client, agentStreamRequest())
		if err != nil {
			t.Fatalf("%s: %v", codec, err)
		}
		if strings.Join(deltas, "") != "Готово, все." || len(deltas) < 1 || len(deltas) > 2 {
			t.Fatalf("%s: deltas %q", codec, deltas)
		}
		unary, err := client.AgentStep(t.Context(), agentRequest())
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(r) != fmt.Sprint(unary.Msg) || r.GetCalls().GetCalls()[0].GetArgumentsJson() != `{"text":"Готово, всё."}` {
			t.Fatalf("%s: result %v, AgentStep gives %v", codec, r, unary.Msg)
		}
	}
	if len(s.cmds) != len(codecs) || !reflect.DeepEqual(s.cmds[0], stepper.cmds[0]) {
		t.Fatalf("commands %+v, want %+v", s.cmds, stepper.cmds)
	}
}

// TestAgentStepStreamErrors: an error ends the stream without a result, with the code and detail of AgentStep, and its
// message never quotes the draft.
func TestAgentStepStreamErrors(t *testing.T) {
	for name, tt := range map[string]struct {
		err    error
		code   connectrpc.Code
		detail hubv1.ErrorCode
	}{
		"provider error after deltas": {err: &agentstep.ProviderFailedError{Cause: &values.ProviderError{Kind: values.KindPermanent, Reason: "invalid tool calls"}},
			code: connectrpc.CodeUnavailable, detail: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR},
		"rate limited":  {err: &agentstep.RateLimitedError{RetryAfter: time.Second}, code: connectrpc.CodeResourceExhausted, detail: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED},
		"conflict":      {err: agentstep.ErrIdempotencyConflict, code: connectrpc.CodeFailedPrecondition, detail: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT},
		"running":       {err: agentstep.ErrAttemptRunning, code: connectrpc.CodeAborted},
		"turn gone":     {err: agentstep.ErrTurnGone, code: connectrpc.CodeFailedPrecondition},
		"bad seal":      {err: agentstep.ErrBadSeal, code: connectrpc.CodeInvalidArgument},
		"hub failure":   {err: errors.New("db down at db.internal:5432"), code: connectrpc.CodeInternal},
		"quota":         {err: &agentstep.QuotaExceededError{Quota: agentstep.Snapshot{Unit: "tokens", Limit: 10}}, code: connectrpc.CodeResourceExhausted, detail: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED},
		"invalid input": {err: agentstep.ErrInvalidRequest, code: connectrpc.CodeInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			srv := agentStreamServer(t, &fakeAgentStreamer{chunks: []string{"draft ", "text"}, err: tt.err}, nil)
			_, r, err := readAgentStream(t, t.Context(), hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL), agentStreamRequest())
			if r != nil {
				t.Fatalf("a result after the error: %v", r)
			}
			code, d, msg := detailOf(t, err)
			if code != tt.code || (tt.detail != hubv1.ErrorCode_ERROR_CODE_UNSPECIFIED && d.GetCode() != tt.detail) {
				t.Fatalf("code %v detail %v, want %v %v", code, d, tt.code, tt.detail)
			}
			for _, leak := range []string{"invalid tool calls", "db.internal", "draft"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("message %q leaks %q", msg, leak)
				}
			}
		})
	}
}

// TestAgentStepStreamExposure: without a streamer the RPC is unimplemented (the desktop falls back to AgentStep); without
// a key it is unauthenticated and never reaches the use case.
func TestAgentStepStreamExposure(t *testing.T) {
	srv := gatewayServer(t, &fakeCompleter{}, nil)
	if _, _, err := readAgentStream(t, t.Context(), hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL), agentStreamRequest()); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
		t.Fatalf("without a streamer: %v", err)
	}
	s := &fakeAgentStreamer{}
	srv = agentStreamServer(t, s, nil)
	req := agentStreamRequest()
	req.Header().Del("Authorization")
	if _, _, err := readAgentStream(t, t.Context(), hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL), req); connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("without a key: %v", err)
	}
	if len(s.cmds) != 0 {
		t.Fatal("an unauthenticated stream reached the use case")
	}
}

// agentService is the real agent step use case over the testenv fakes, provider streaming on.
func agentService(t *testing.T, steps ...testenv.AgentProviderStep) (*agentstep.Service, *testenv.QuotaStore, *testenv.FakeAgentProvider) {
	t.Helper()
	models, err := modelallowlist.New([]modelallowlist.Entry{{Alias: "planner", Model: "claude-planner", MaxOutput: 4096}})
	if err != nil {
		t.Fatal(err)
	}
	system, catalog, err := testenv.PanelV1()
	if err != nil {
		t.Fatal(err)
	}
	sealer, err := testenv.Sealer(1)
	if err != nil {
		t.Fatal(err)
	}
	store, provider := testenv.NewQuotaStore(), testenv.NewFakeAgentProvider(steps...)
	svc, err := agentstep.New(agentstep.Deps{
		Accounts: store, Turns: testenv.NewAgentTurns(), Provider: provider, Streamer: provider, Limiter: &testenv.FakeLimiter{},
		Clock: testenv.NewFakeClock(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)), Random: testenv.Random{}, Models: models,
		Sealer: sealer, Log: slog.New(slog.DiscardHandler),
	}, agentstep.Config{
		Unit: "tokens", Limit: 100_000, ReservationTTL: 10 * time.Minute, KeyRetention: 24 * time.Hour, CallTimeout: 5 * time.Minute,
		Profiles:       []agentstep.Profile{{Name: values.ProfilePanel, Alias: "planner", Versions: []agentstep.Version{{System: system, Catalog: catalog}}}},
		ProviderStream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, provider
}

func replyStep(t *testing.T, text string, chunks ...string) testenv.AgentProviderStep {
	t.Helper()
	u, err := values.NewUsage(900, 120)
	if err != nil {
		t.Fatal(err)
	}
	call, err := values.NewToolCall("toolu_01", "reply", `{"text":"`+text+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	return testenv.AgentProviderStep{Answer: values.NewAgentAnswer([]values.ToolCall{call}, u, true), Chunks: chunks}
}

func studentRequest(key string) *connectrpc.Request[hubv1.AgentStepStreamRequest] {
	return withKey(connectrpc.NewRequest(&hubv1.AgentStepStreamRequest{Request: &hubv1.AgentStepRequest{
		IdempotencyKey: key, ThreadId: "th", Profile: hubv1.AgentProfile_AGENT_PROFILE_PANEL, CatalogVersion: 1,
		Base:  &hubv1.AgentBase{Subjects: []*hubv1.AgentSubject{{SubjectId: "s1", Name: "Матан"}}, Today: "2026-10-28", TimeZone: "Europe/Moscow"},
		Scope: &hubv1.AgentScope{SubjectId: "s1"},
		Turns: []*hubv1.AgentTurn{{Turn: &hubv1.AgentTurn_StudentText{StudentText: "что задали?"}}},
	}}), anyKey)
}

// TestAgentStepStreamClientCancelsMidStream runs the real use case behind the handler: the client goes away after the
// first delta (the desktop's «Стоп»); the Hub reads the provider to the end, settles once in full and stores the step;
// the next call with the key gets the result alone, replayed free.
func TestAgentStepStreamClientCancelsMidStream(t *testing.T) {
	wait, entered := make(chan struct{}), make(chan struct{})
	step := replyStep(t, "Два задания.", "Два ", "задания.")
	step.Wait, step.Entered = wait, entered
	svc, store, provider := agentService(t, step)
	done := make(chan error, 4)
	srv := agentStreamServer(t, notifyingAgentStreamer{AgentStreamer: svc, done: done}, nil)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.AgentStepStream(ctx, studentRequest("ag:th:1:1"))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() || stream.Msg().GetDelta() != "Два " {
		t.Fatalf("first message %v, err %v; want the first chunk", stream.Msg(), stream.Err())
	}
	<-entered
	cancel()
	_ = stream.Close()
	close(wait)
	if err := <-done; err != nil {
		t.Fatalf("the use case ended with %v after the cancel, want the step finished", err)
	}
	if used, held := store.Totals("00000000000000aa"); used != 1020 || held != 0 {
		t.Fatalf("used %d held %d, want one full settlement", used, held)
	}

	deltas, r, err := readAgentStream(t, t.Context(), client, studentRequest("ag:th:1:1"))
	if err != nil || !r.GetReplayed() || len(deltas) != 0 || r.GetCalls().GetCalls()[0].GetArgumentsJson() != `{"text":"Два задания."}` {
		t.Fatalf("retry deltas %q, result %v, err %v", deltas, r, err)
	}
	if len(provider.Calls()) != 1 {
		t.Fatalf("provider calls = %d", len(provider.Calls()))
	}
}
