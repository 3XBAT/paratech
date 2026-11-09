package hubclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// fakeStreamHub serves AgentStepStream with serve, and AgentStep (the fallback) through the embedded fake. serve runs
// on the server goroutine and must not call t.Fatal.
type fakeStreamHub struct {
	fakeAgentHub

	smu     sync.Mutex
	streams []*hubv1.AgentStepStreamRequest
	serve   func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error
}

func (f *fakeStreamHub) AgentStepStream(ctx context.Context, r *connect.Request[hubv1.AgentStepStreamRequest],
	s *connect.ServerStream[hubv1.AgentStepStreamResponse],
) error {
	f.smu.Lock()
	f.streams = append(f.streams, r.Msg)
	f.smu.Unlock()
	return f.serve(ctx, s.Send)
}

func (f *fakeStreamHub) streamed() []*hubv1.AgentStepStreamRequest {
	f.smu.Lock()
	defer f.smu.Unlock()
	return append([]*hubv1.AgentStepStreamRequest(nil), f.streams...)
}

func stepDelta(s string) *hubv1.AgentStepStreamResponse {
	return &hubv1.AgentStepStreamResponse{Event: &hubv1.AgentStepStreamResponse_Delta{Delta: s}}
}

func stepHeartbeat() *hubv1.AgentStepStreamResponse {
	return &hubv1.AgentStepStreamResponse{Event: &hubv1.AgentStepStreamResponse_Heartbeat{Heartbeat: &hubv1.StreamHeartbeat{}}}
}

func stepResult(r *hubv1.AgentStepResponse) *hubv1.AgentStepStreamResponse {
	return &hubv1.AgentStepStreamResponse{Event: &hubv1.AgentStepStreamResponse_Result{Result: r}}
}

// serveAll sends msgs in order, then returns end.
func serveAll(end error, msgs ...*hubv1.AgentStepStreamResponse) func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error {
	return func(_ context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
		for _, m := range msgs {
			if err := send(m); err != nil {
				return err
			}
		}
		return end
	}
}

func okResponse() *hubv1.AgentStepResponse {
	r, _ := agentOK()
	return r
}

type drafts struct{ got []string }

func (d *drafts) add(s string) { d.got = append(d.got, s) }

// The step reads AgentStepStream: each delta hands the whole text so far to the draft, the reply is the result, and the
// stream carries exactly the AgentStepRequest the unary call would.
func TestStepStreamsTheReplyDraft(t *testing.T) {
	f := &fakeStreamHub{serve: serveAll(nil, stepDelta("При"), stepDelta(""), stepDelta("вет,\n"), stepDelta("мир"), stepResult(okResponse()))}
	c := startGateway(t, f)
	d := &drafts{}

	got, err := c.Step(t.Context(), stepRequest(t), d.add)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !got.Replayed || len(got.Calls.Calls) != 1 || got.Calls.Calls[0].Tool != "list_subjects" {
		t.Fatalf("reply = %+v", got)
	}
	if want := []string{"При", "Привет,\n", "Привет,\nмир"}; strings.Join(d.got, "|") != strings.Join(want, "|") {
		t.Fatalf("drafts = %q, want %q", d.got, want)
	}
	reqs := f.streamed()
	want, err := agentRequestOf(stepRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || !proto.Equal(reqs[0], &hubv1.AgentStepStreamRequest{Request: want, Heartbeat: true}) {
		t.Fatalf("stream requests = %v", reqs)
	}
	if len(f.seen()) != 0 {
		t.Fatal("a streaming Hub got the unary call too")
	}
}

// A replay (or a Hub with provider_stream off) sends only the result: no draft, the same reply. A nil draft is fine.
func TestStepStreamWithoutDeltas(t *testing.T) {
	c := startGateway(t, &fakeStreamHub{serve: serveAll(nil, stepResult(okResponse()))})
	d := &drafts{}
	if _, err := c.Step(t.Context(), stepRequest(t), d.add); err != nil || len(d.got) != 0 {
		t.Fatalf("Step = %v, drafts %q", err, d.got)
	}
	if _, err := c.Step(t.Context(), stepRequest(t), nil); err != nil {
		t.Fatalf("Step without a draft: %v", err)
	}
}

// A Hub without AgentStepStream (unimplemented) is asked through AgentStep with the same request, so the same key.
func TestStepFallsBackToUnary(t *testing.T) {
	f := &fakeStreamHub{
		fakeAgentHub: fakeAgentHub{answer: agentOK},
		serve:        serveAll(connect.NewError(connect.CodeUnimplemented, errors.New("older hub"))),
	}
	c := startGateway(t, f)
	got, err := c.Step(t.Context(), stepRequest(t), (&drafts{}).add)
	if err != nil || len(got.Calls.Calls) != 1 {
		t.Fatalf("Step = %+v, %v", got, err)
	}
	streamed, unary := f.streamed(), f.seen()
	if len(streamed) != 1 || len(unary) != 1 || !proto.Equal(streamed[0].GetRequest(), unary[0]) ||
		unary[0].GetIdempotencyKey() != "ag:th1:1:2" {
		t.Fatalf("stream %v, unary %v", streamed, unary)
	}
}

// The stream carries the scope mentions of the unary request, and the fallback to AgentStep sends the same mentions
// under the same key.
func TestStepStreamSendsTheMentions(t *testing.T) {
	want := []*hubv1.AgentMention{
		{Kind: hubv1.AgentMentionKind_AGENT_MENTION_KIND_LESSON, Id: "l0", Current: true},
		{Kind: hubv1.AgentMentionKind_AGENT_MENTION_KIND_LESSON, Id: "l1"},
	}
	tests := []struct {
		name     string
		serve    func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error
		fallback bool
	}{
		{"streaming hub", serveAll(nil, stepResult(okResponse())), false},
		{"older hub", serveAll(connect.NewError(connect.CodeUnimplemented, errors.New("older hub"))), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStreamHub{fakeAgentHub: fakeAgentHub{answer: agentOK}, serve: tt.serve}
			c := startGateway(t, f)
			req := stepRequest(t)
			req.Mentions = lessonMentions(t, 2, 1)

			if _, err := c.Step(t.Context(), req, nil); err != nil {
				t.Fatalf("Step: %v", err)
			}
			streamed := f.streamed()
			if len(streamed) != 1 {
				t.Fatalf("%d stream requests", len(streamed))
			}
			assertMentions(t, streamed[0].GetRequest().GetScope().GetMentions(), want)

			unary := f.seen()
			if !tt.fallback {
				if len(unary) != 0 {
					t.Fatal("a streaming Hub got the unary call too")
				}
				return
			}
			if len(unary) != 1 || unary[0].GetIdempotencyKey() != streamed[0].GetRequest().GetIdempotencyKey() {
				t.Fatalf("unary %v after stream %v", unary, streamed)
			}
			assertMentions(t, unary[0].GetScope().GetMentions(), want)
		})
	}
}

func assertMentions(t *testing.T, got, want []*hubv1.AgentMention) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("mentions = %v, want %v", got, want)
	}
	for i := range want {
		if !proto.Equal(got[i], want[i]) {
			t.Fatalf("mention %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// After an unimplemented AgentStepStream the client remembers the older Hub: the next step goes straight to AgentStep,
// without another refused stream call.
func TestStepRemembersAnOlderHub(t *testing.T) {
	f := &fakeStreamHub{
		fakeAgentHub: fakeAgentHub{answer: agentOK},
		serve:        serveAll(connect.NewError(connect.CodeUnimplemented, errors.New("older hub"))),
	}
	c := startGateway(t, f)
	for i := range 2 {
		if _, err := c.Step(t.Context(), stepRequest(t), (&drafts{}).add); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if s, u := len(f.streamed()), len(f.seen()); s != 1 || u != 2 {
		t.Fatalf("%d stream calls, %d unary calls; want 1 and 2", s, u)
	}
}

// Other stream failures are not remembered: the next step streams again.
func TestStepKeepsStreamingAfterOtherFailures(t *testing.T) {
	var calls atomic.Int32
	f := &fakeStreamHub{
		fakeAgentHub: fakeAgentHub{answer: agentOK},
		serve: func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
			if calls.Add(1) == 1 {
				return connect.NewError(connect.CodeAborted, errors.New("running"))
			}
			return serveAll(nil, stepResult(okResponse()))(ctx, send)
		},
	}
	c := startGateway(t, f)
	if _, err := c.Step(t.Context(), stepRequest(t), nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("first step: %v", err)
	}
	if _, err := c.Step(t.Context(), stepRequest(t), nil); err != nil {
		t.Fatalf("second step: %v", err)
	}
	if s, u := len(f.streamed()), len(f.seen()); s != 2 || u != 0 {
		t.Fatalf("%d stream calls, %d unary calls; want 2 and 0", s, u)
	}
}

// The stream's failures map exactly as AgentStep's, whether they come at once or after deltas; a stream that ends
// without a result is a lost answer (transient, the automatic retry replays the key); messages after the result are malformed.
func TestStepStreamErrors(t *testing.T) {
	providerDown := detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
		Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "x",
	})
	tests := []struct {
		name   string
		serve  func(context.Context, func(*hubv1.AgentStepStreamResponse) error) error
		want   []error
		absent []error
	}{
		{
			name:  "turn gone",
			serve: serveAll(connect.NewError(connect.CodeFailedPrecondition, errors.New("start again"))),
			want:  []error{ErrTurnGone, agent.ErrTurnGone, backoff.ErrPermanent},
		},
		{
			name:  "turn gone after deltas",
			serve: serveAll(connect.NewError(connect.CodeFailedPrecondition, errors.New("start again")), stepDelta("a")),
			want:  []error{ErrTurnGone, agent.ErrTurnGone},
		},
		{
			name: "idempotency conflict",
			serve: serveAll(detailErr(connect.CodeFailedPrecondition, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, Message: "conflict",
			})),
			want: []error{ErrIdempotencyConflict, backoff.ErrPermanent}, absent: []error{agent.ErrTurnGone},
		},
		{
			name:  "provider failed for good after deltas",
			serve: serveAll(providerDown, stepDelta("половина"), stepDelta(" ответа")),
			want:  []error{ErrProvider, backoff.ErrPermanent},
		},
		{
			name: "provider retryable",
			serve: serveAll(detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true, Message: "busy",
			}), stepDelta("a")),
			want: []error{ErrProvider, backoff.ErrTransient, shared.ErrServiceUnavailable},
		},
		{
			name:  "quota",
			serve: serveAll(detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: "q"})),
			want:  []error{shared.ErrQuotaExceeded},
		},
		{name: "busy", serve: serveAll(connect.NewError(connect.CodeAborted, errors.New("running"))), want: []error{ErrBusy, backoff.ErrTransient}},
		{name: "key", serve: serveAll(connect.NewError(connect.CodeUnauthenticated, errors.New("key"))), want: []error{ErrInvalidAPIKey}},
		{
			name:  "no result",
			serve: serveAll(nil, stepDelta("всё"), stepDelta(" почти")),
			want:  []error{ErrUnavailable, backoff.ErrTransient, shared.ErrServiceUnavailable},
		},
		{
			name: "empty stream", serve: serveAll(nil),
			want: []error{ErrUnavailable, backoff.ErrTransient},
		},
		{
			name: "delta after the result", serve: serveAll(nil, stepResult(okResponse()), stepDelta("x")),
			want: []error{ErrBadResponse, backoff.ErrPermanent},
		},
		{
			name: "heartbeat after the result", serve: serveAll(nil, stepResult(okResponse()), stepHeartbeat()),
			want: []error{ErrBadResponse, backoff.ErrPermanent},
		},
		{
			name: "only heartbeats", serve: serveAll(nil, stepHeartbeat(), stepHeartbeat()),
			want: []error{ErrUnavailable, backoff.ErrTransient},
		},
		{
			name: "two results", serve: serveAll(nil, stepResult(okResponse()), stepResult(okResponse())),
			want: []error{ErrBadResponse, backoff.ErrPermanent},
		},
		{
			name: "bad result", serve: serveAll(nil, stepResult(&hubv1.AgentStepResponse{Calls: &hubv1.SealedCalls{Seal: []byte{1}}})),
			want: []error{ErrBadResponse, backoff.ErrPermanent},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeStreamHub{serve: tt.serve}
			c := startGateway(t, f)
			_, err := c.Step(t.Context(), stepRequest(t), (&drafts{}).add)
			if err == nil {
				t.Fatal("Step went through")
			}
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
			if len(f.seen()) != 0 {
				t.Error("a failed stream fell back to the unary call")
			}
		})
	}
}

// The caller going away (a cut stream) ends the call at once with the caller's error, after the drafts so far.
func TestStepStreamCancelledByTheCaller(t *testing.T) {
	sent := make(chan struct{})
	f := &fakeStreamHub{serve: func(ctx context.Context, send func(*hubv1.AgentStepStreamResponse) error) error {
		if err := send(stepDelta("начало")); err != nil {
			return err
		}
		close(sent)
		<-ctx.Done()
		return ctx.Err()
	}}
	c := startGateway(t, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := &drafts{}
	var got []string
	_, err := c.Step(ctx, stepRequest(t), func(s string) {
		d.add(s)
		got = d.got
		cancel() // the first draft is seen: the student presses «Стоп»
	})
	<-sent
	if !errors.Is(err, context.Canceled) || len(got) != 1 || got[0] != "начало" {
		t.Fatalf("Step = %v, drafts %q", err, got)
	}
}

// The draft is display text: valid UTF-8 (a rune split across deltas waits for its rest), no control characters but
// line breaks and tabs, at most maxReplyDraftRunes characters.
func TestReplyDraftBounds(t *testing.T) {
	long := strings.Repeat("я", maxReplyDraftRunes-1)
	tests := []struct {
		name    string
		deltas  []string
		want    string
		changes int
	}{
		{"split rune", []string{"Пр\xd0", "\xb8вет"}, "Привет", 2},
		{"broken bytes", []string{"a\xffb", "\xc3", "(c"}, "ab(c", 2},
		{"controls", []string{"a\x00b\r\n\tc\x1b[31m\u0085"}, "ab\n\tc[31m", 1},
		{"replacement char kept", []string{"a\uFFFDb"}, "a\uFFFDb", 1},
		{"bounded", []string{long, "яб", "в"}, long + "я", 2},
		{"only controls", []string{"\x00\x01"}, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d replyDraft
			changes := 0
			for _, s := range tt.deltas {
				if d.add(s) {
					changes++
				}
			}
			if d.String() != tt.want || changes != tt.changes || !utf8.ValidString(d.String()) {
				t.Fatalf("draft %q (%d changes), want %q (%d)", d.String(), changes, tt.want, tt.changes)
			}
			if n := utf8.RuneCountInString(d.String()); n > maxReplyDraftRunes {
				t.Fatalf("draft of %d characters", n)
			}
		})
	}
}

// Deltas of the stream itself are bounded the same way.
func TestStepStreamBoundsTheDraft(t *testing.T) {
	big := strings.Repeat("ы", 3000)
	f := &fakeStreamHub{serve: serveAll(nil, stepDelta(big), stepDelta(big), stepDelta("\x00конец"), stepResult(okResponse()))}
	c := startGateway(t, f)
	d := &drafts{}
	if _, err := c.Step(t.Context(), stepRequest(t), d.add); err != nil {
		t.Fatal(err)
	}
	if len(d.got) != 2 || utf8.RuneCountInString(d.got[1]) != maxReplyDraftRunes {
		t.Fatalf("%d drafts, last of %d characters", len(d.got), utf8.RuneCountInString(d.got[len(d.got)-1]))
	}
}
