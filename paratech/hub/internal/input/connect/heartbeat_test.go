package connect_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/cases/agentstep"
	"paratech/hub/internal/cases/gateway"
	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/input/connect"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// silentStreamer is a provider that is silent until release is closed (a nil channel never fires), then sends chunks
// and answers with a fixed result or err. It serves both CompleteStream and AgentStepStream.
type silentStreamer struct {
	release <-chan struct{}
	chunks  []string
	err     error
}

func (s silentStreamer) play(ctx context.Context, onDelta func(string)) error {
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	for _, c := range s.chunks {
		onDelta(c)
	}
	return s.err
}

func (s silentStreamer) CompleteStream(ctx context.Context, _ gateway.Command, onDelta func(string)) (gateway.Result, error) {
	res := gateway.Result{Output: "{}", Quota: gateway.Snapshot{Unit: "tokens", Remaining: 900, Limit: 1000}}
	return res, s.play(ctx, onDelta)
}

func (s silentStreamer) StepStream(ctx context.Context, _ agentstep.Command, onDelta func(string)) (agentstep.Result, error) {
	res := agentstep.Result{
		Calls: []agentstep.Call{{ID: "toolu_1", Tool: "reply", Arguments: `{"text":"Готово."}`}}, Seal: []byte{1},
		Quota: agentstep.Snapshot{Unit: "tokens", Remaining: 50, Limit: 100},
	}
	return res, s.play(ctx, onDelta)
}

// silence is the test-driven clock of a stream: no wall-clock time passes in these tests. tick makes silences elapse,
// so heartbeats go out; the provider stays silent until the test closes release.
type silence struct {
	ticks   chan time.Time
	release chan struct{}
}

func newSilence() *silence {
	return &silence{ticks: make(chan time.Time), release: make(chan struct{})}
}

// tick fires n silences in the background; the unbuffered channel hands a tick over only when the stream waits for it.
func (s *silence) tick(t *testing.T, n int) {
	t.Helper()
	go func() {
		for range n {
			select {
			case s.ticks <- time.Time{}:
			case <-t.Context().Done():
				return
			}
		}
	}()
}

// onBeats returns an event hook that closes release once the client has read n heartbeats, so the provider answers
// only after exactly n heartbeats were sent.
func (s *silence) onBeats(n int) func(string) {
	seen := 0
	return func(e string) {
		if e == evHeartbeat {
			if seen++; seen == n {
				close(s.release)
			}
		}
	}
}

// heartbeatClient serves g's handler, the stream silences coming from clk when it is set, and returns a client of it.
func heartbeatClient(t *testing.T, svc connect.Completer, clk *silence, opts ...connect.GatewayOption) hubv1connect.GatewayServiceClient {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	if clk != nil {
		opts = append(opts, connect.WithHeartbeatTicks(clk.ticks))
	}
	g, err := connect.NewGateway(svc, log, opts...)
	if err != nil {
		t.Fatal(err)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: fakeProbe{}, Auth: acceptAll{}, Gateway: g})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
}

// Events of a stream as read: "hb", "d:<text>" or "result"; a message after the result fails the test.
const (
	evHeartbeat = "hb"
	evResult    = "result"
)

func completeEvents(t *testing.T, client hubv1connect.GatewayServiceClient, req *connectrpc.Request[hubv1.CompleteStreamRequest], onEvent ...func(string)) ([]string, *hubv1.CompletionResult, error) {
	t.Helper()
	stream, err := client.CompleteStream(t.Context(), req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = stream.Close() }()
	var (
		events []string
		result *hubv1.CompletionResult
	)
	for stream.Receive() {
		if result != nil {
			t.Fatal("a message after the result")
		}
		switch e := stream.Msg().GetEvent().(type) {
		case *hubv1.CompleteStreamResponse_Heartbeat:
			events = append(events, evHeartbeat)
		case *hubv1.CompleteStreamResponse_Delta:
			events = append(events, "d:"+e.Delta)
		case *hubv1.CompleteStreamResponse_Result:
			result = e.Result
			events = append(events, evResult)
		default:
			t.Fatalf("event %T", e)
		}
		for _, f := range onEvent {
			f(events[len(events)-1])
		}
	}
	return events, result, stream.Err()
}

func agentEvents(t *testing.T, client hubv1connect.GatewayServiceClient, req *connectrpc.Request[hubv1.AgentStepStreamRequest], onEvent ...func(string)) ([]string, *hubv1.AgentStepResponse, error) {
	t.Helper()
	stream, err := client.AgentStepStream(t.Context(), req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = stream.Close() }()
	var (
		events []string
		result *hubv1.AgentStepResponse
	)
	for stream.Receive() {
		if result != nil {
			t.Fatal("a message after the result")
		}
		switch e := stream.Msg().GetEvent().(type) {
		case *hubv1.AgentStepStreamResponse_Heartbeat:
			events = append(events, evHeartbeat)
		case *hubv1.AgentStepStreamResponse_Delta:
			events = append(events, "d:"+e.Delta)
		case *hubv1.AgentStepStreamResponse_Result:
			result = e.Result
			events = append(events, evResult)
		default:
			t.Fatalf("event %T", e)
		}
		for _, f := range onEvent {
			f(events[len(events)-1])
		}
	}
	return events, result, stream.Err()
}

// Names of the two streaming RPCs under test.
const (
	rpcComplete = "CompleteStream"
	rpcAgent    = "AgentStepStream"
)

// streams returns one runner per RPC. A run opens a stream with the heartbeat flag set to on against a provider that
// mk builds for it; the provider stays silent while `beats` silences are fired, and answers once the client has read
// that many heartbeats (at once when beats is 0). The tests therefore see exactly `beats` heartbeats, whatever the
// speed of the machine.
func streams(t *testing.T, mk func(rpc string, release <-chan struct{}) silentStreamer, on bool, beats int) map[string]func() ([]string, bool, error) {
	t.Helper()
	setup := func(rpc string) (*silence, hubv1connect.GatewayServiceClient, func(string)) {
		clk := newSilence()
		s := mk(rpc, clk.release)
		client := heartbeatClient(t, &fakeCompleter{}, clk, connect.WithCompleteStream(s), connect.WithAgentStepStream(s))
		if beats == 0 {
			close(clk.release)
			return clk, client, func(string) {}
		}
		clk.tick(t, beats)
		return clk, client, clk.onBeats(beats)
	}
	return map[string]func() ([]string, bool, error){
		rpcComplete: func() ([]string, bool, error) {
			_, client, hook := setup(rpcComplete)
			req := streamRequest("ans-1", "data")
			req.Msg.Heartbeat = on
			events, r, err := completeEvents(t, client, req, hook)
			return events, r != nil, err
		},
		rpcAgent: func() ([]string, bool, error) {
			_, client, hook := setup(rpcAgent)
			req := agentStreamRequest()
			req.Msg.Heartbeat = on
			events, r, err := agentEvents(t, client, req, hook)
			return events, r != nil, err
		},
	}
}

// split counts the heartbeats of events and joins the deltas; it fails the test when a heartbeat follows a delta.
func split(t *testing.T, events []string) (beats int, text string) {
	t.Helper()
	var b strings.Builder
	for i, e := range events {
		switch {
		case e == evHeartbeat:
			if i != beats {
				t.Fatalf("a heartbeat after a delta: %q", events)
			}
			beats++
		case strings.HasPrefix(e, "d:"):
			b.WriteString(e[2:])
		}
	}
	return beats, b.String()
}

// TestStreamHeartbeatDuringSilence: a provider silent for two driven silences gets exactly two heartbeats, then its
// deltas and the result; nothing follows the result.
func TestStreamHeartbeatDuringSilence(t *testing.T) {
	mk := func(_ string, release <-chan struct{}) silentStreamer {
		return silentStreamer{release: release, chunks: []string{"Два ", "задания."}}
	}
	for name, run := range streams(t, mk, true, 2) {
		t.Run(name, func(t *testing.T) {
			events, result, err := run()
			if err != nil || !result {
				t.Fatalf("events %q, err %v; want a result", events, err)
			}
			beats, text := split(t, events)
			if beats != 2 || text != "Два задания." || events[len(events)-1] != evResult {
				t.Fatalf("events %q: %d heartbeats, deltas %q", events, beats, text)
			}
		})
	}
}

// TestStreamHeartbeatThenProviderError: an error after a heartbeat is a Connect error with its detail and no result.
func TestStreamHeartbeatThenProviderError(t *testing.T) {
	cause := &values.ProviderError{Kind: values.KindTransient, Reason: "overloaded"}
	mk := func(rpc string, release <-chan struct{}) silentStreamer {
		if rpc == rpcAgent {
			return silentStreamer{release: release, err: &agentstep.ProviderFailedError{Transient: true, Cause: cause}}
		}
		return silentStreamer{release: release, err: &gateway.ProviderFailedError{Transient: true, Cause: cause}}
	}
	for name, run := range streams(t, mk, true, 1) {
		t.Run(name, func(t *testing.T) {
			events, result, err := run()
			if result || !slices.Equal(events, []string{evHeartbeat}) {
				t.Fatalf("events %q, result %t; want one heartbeat and no result", events, result)
			}
			if code, d, _ := detailOf(t, err); code != connectrpc.CodeUnavailable || d.GetCode() != hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR {
				t.Fatalf("code %v detail %v", code, d)
			}
		})
	}
}

// TestStreamWithoutHeartbeatFlag: without the flag a silent provider gets no heartbeat (old clients see nothing new).
func TestStreamWithoutHeartbeatFlag(t *testing.T) {
	mk := func(_ string, release <-chan struct{}) silentStreamer {
		return silentStreamer{release: release, chunks: []string{"Готово."}}
	}
	for name, run := range streams(t, mk, false, 0) {
		t.Run(name, func(t *testing.T) {
			events, result, err := run()
			if err != nil || !result || !slices.Equal(events, []string{"d:Готово.", evResult}) {
				t.Fatalf("events %q, result %t, err %v", events, result, err)
			}
		})
	}
}

// TestCompleteStreamHeartbeatFlagIsNotPartOfTheRequest: the same key with and without the flag is one logical request:
// the second call is a free replay, never IDEMPOTENCY_CONFLICT, and a replay gets no heartbeat.
func TestCompleteStreamHeartbeatFlagIsNotPartOfTheRequest(t *testing.T) {
	step := completionStep(t, strings.Join(streamChunks, ""), streamChunks)
	svc, store, provider := answerService(t, true, slog.New(slog.DiscardHandler), step, step)
	client := heartbeatClient(t, svc, nil, connect.WithCompleteStream(svc))

	for key, flags := range map[string][2]bool{"ans-1": {false, true}, "ans-2": {true, false}} {
		first := streamRequest(key, streamInput)
		first.Msg.Heartbeat = flags[0]
		if _, r, err := completeEvents(t, client, first); err != nil || r.GetReplayed() {
			t.Fatalf("%s: first call %v, %v", key, r, err)
		}
		used, _ := store.Totals("00000000000000aa")

		again := streamRequest(key, streamInput)
		again.Msg.Heartbeat = flags[1]
		events, r, err := completeEvents(t, client, again)
		if err != nil || !r.GetReplayed() || r.GetOutput() != streamAnswer || slices.Contains(events, evHeartbeat) {
			t.Fatalf("%s: replay events %q, result %v, err %v", key, events, r, err)
		}
		if now, _ := store.Totals("00000000000000aa"); now != used {
			t.Fatalf("%s: the replay was charged: %d -> %d", key, used, now)
		}
	}
	if len(provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want one per key", len(provider.Calls()))
	}
}

// TestAgentStepStreamHeartbeatFlagIsNotPartOfTheRequest: as for CompleteStream; the fingerprint covers AgentStepRequest
// alone.
func TestAgentStepStreamHeartbeatFlagIsNotPartOfTheRequest(t *testing.T) {
	svc, store, provider := agentService(t, replyStep(t, "Два задания.", "Два ", "задания."), replyStep(t, "Одно.", "Одно."))
	client := heartbeatClient(t, &fakeCompleter{}, nil, connect.WithAgentStepStream(svc))

	for key, flags := range map[string][2]bool{"ag:th:1:1": {false, true}, "ag:th:2:1": {true, false}} {
		first := studentRequest(key)
		first.Msg.Heartbeat = flags[0]
		if _, r, err := agentEvents(t, client, first); err != nil || r.GetReplayed() {
			t.Fatalf("%s: first call %v, %v", key, r, err)
		}
		used, _ := store.Totals("00000000000000aa")

		again := studentRequest(key)
		again.Msg.Heartbeat = flags[1]
		events, r, err := agentEvents(t, client, again)
		if err != nil || !r.GetReplayed() || !slices.Equal(events, []string{evResult}) {
			t.Fatalf("%s: replay events %q, result %v, err %v", key, events, r, err)
		}
		if now, _ := store.Totals("00000000000000aa"); now != used {
			t.Fatalf("%s: the replay was charged: %d -> %d", key, used, now)
		}
	}
	if len(provider.Calls()) != 2 {
		t.Fatalf("provider calls = %d, want one per key", len(provider.Calls()))
	}
}
