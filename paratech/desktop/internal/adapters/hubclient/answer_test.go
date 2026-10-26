package hubclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

func answerRequest(t *testing.T) answer.Request {
	t.Helper()
	r, err := answer.NewRequest("Что такое производная?", []answer.Excerpt{{
		Text:   "Производная — предел отношения приращений.",
		Source: answer.Source{Recording: "rec1", Section: answer.SectionLectureText, StartMs: 1000},
	}}, "ag:th1:1")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func answerSpec(req answer.Request) *hubv1.CompletionSpec {
	return &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_ANSWER,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: answer.MaxOutputTokens,
	}
}

// streamHub is a Hub with CompleteStream: stream sends the messages of the n-th stream call and returns its end error.
// Complete is the unary fakeHub. It runs on the server goroutine, so it must not call t.Fatal.
type streamHub struct {
	*fakeHub

	smu     sync.Mutex
	sreqs   []*hubv1.CompleteStreamRequest
	sheader []http.Header
	stream  func(ctx context.Context, n int, send func(*hubv1.CompleteStreamResponse) error) error
}

func (h *streamHub) CompleteStream(ctx context.Context, r *connect.Request[hubv1.CompleteStreamRequest],
	s *connect.ServerStream[hubv1.CompleteStreamResponse],
) error {
	h.smu.Lock()
	h.sreqs = append(h.sreqs, r.Msg)
	h.sheader = append(h.sheader, r.Header().Clone())
	n := len(h.sreqs)
	h.smu.Unlock()
	return h.stream(ctx, n, s.Send)
}

func (h *streamHub) streams() ([]*hubv1.CompleteStreamRequest, []http.Header) {
	h.smu.Lock()
	defer h.smu.Unlock()
	return slices.Clone(h.sreqs), slices.Clone(h.sheader)
}

func delta(s string) *hubv1.CompleteStreamResponse {
	return &hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Delta{Delta: s}}
}

func result(out string) *hubv1.CompleteStreamResponse {
	return &hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Result{Result: &hubv1.CompletionResult{
		Output: out, Usage: &hubv1.Usage{InputTokens: 10, OutputTokens: 5},
	}}}
}

// sends returns a stream that sends msgs, then ends with end.
func sends(end error, msgs ...*hubv1.CompleteStreamResponse) func(context.Context, int, func(*hubv1.CompleteStreamResponse) error) error {
	return func(_ context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
		for _, m := range msgs {
			if err := send(m); err != nil {
				return err
			}
		}
		return end
	}
}

// startStreamHub serves h over TLS like startHub.
func startStreamHub(t *testing.T, h *streamHub) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(h))
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

// partials records what the callback got.
type partials struct{ got []string }

func (p *partials) add(s string) { p.got = append(p.got, s) }

// The stream sends the contract fields under the same key; the deltas reach the callback accumulated and in order, and
// the result, not the deltas, is the output.
func TestAnswerQuestionStreams(t *testing.T) {
	const out = `{"claims":[],"missing":["Всё"]}`
	h := &streamHub{fakeHub: &fakeHub{}, stream: sends(nil,
		delta("```json\n"), delta(`{"claims":[],`), delta(""), delta(`"missing":["Всё"]}`), result(out))}
	c := startStreamHub(t, h)
	req := answerRequest(t)

	p := &partials{}
	got, err := c.AnswerQuestion(t.Context(), req, p.add)
	if err != nil || got != out {
		t.Fatalf("AnswerQuestion = %q, %v", got, err)
	}
	want := []string{"```json\n", "```json\n" + `{"claims":[],`, "```json\n" + `{"claims":[],"missing":["Всё"]}`}
	if !slices.Equal(p.got, want) {
		t.Fatalf("partials %q, want %q", p.got, want)
	}

	// A nil callback reads the same; a retry sends the same key.
	if got, err := c.AnswerQuestion(t.Context(), req, nil); err != nil || got != out {
		t.Fatalf("no callback = %q, %v", got, err)
	}
	sreqs, headers := h.streams()
	wantReq := &hubv1.CompleteStreamRequest{Spec: answerSpec(req)}
	if len(sreqs) != 2 || !proto.Equal(sreqs[0], wantReq) || !proto.Equal(sreqs[1], wantReq) {
		t.Fatalf("requests on the wire:\n got %v\nwant %v", sreqs, wantReq)
	}
	if headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatal("no API key")
	}
	if reqs, _ := h.calls(); len(reqs) != 0 {
		t.Fatalf("%d unary calls next to a stream", len(reqs))
	}
}

// A replay is the stored output as one delta, then the result.
func TestAnswerQuestionStreamReplay(t *testing.T) {
	const out = `{"claims":[],"missing":["Всё"]}`
	c := startStreamHub(t, &streamHub{fakeHub: &fakeHub{}, stream: sends(nil, delta(out), result(out))})
	p := &partials{}
	if got, err := c.AnswerQuestion(t.Context(), answerRequest(t), p.add); err != nil || got != out {
		t.Fatalf("AnswerQuestion = %q, %v", got, err)
	}
	if !slices.Equal(p.got, []string{out}) {
		t.Fatalf("partials %q", p.got)
	}
}

// A Hub without CompleteStream is read through Complete with the same spec and key.
func TestAnswerQuestionFallsBackToComplete(t *testing.T) {
	const out = `{"claims":[],"missing":["Всё"]}`
	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: out}}, nil
	}}
	h := &streamHub{fakeHub: f, stream: sends(connect.NewError(connect.CodeUnimplemented, errors.New("not here")))}
	c := startStreamHub(t, h)
	req := answerRequest(t)

	p := &partials{}
	if got, err := c.AnswerQuestion(t.Context(), req, p.add); err != nil || got != out {
		t.Fatalf("AnswerQuestion = %q, %v", got, err)
	}
	if len(p.got) != 0 {
		t.Fatalf("partials of a unary read: %q", p.got)
	}
	reqs, headers := f.calls()
	sreqs, _ := h.streams()
	want := &hubv1.CompleteRequest{Spec: answerSpec(req)}
	if len(sreqs) != 1 || len(reqs) != 1 || !proto.Equal(reqs[0], want) || headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("%d streams, unary %v", len(sreqs), reqs)
	}
}

// A failed stream returns no output, whatever deltas came; the error classes are those of Complete.
func TestAnswerQuestionStreamErrors(t *testing.T) {
	refusal := detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR})
	tests := []struct {
		name   string
		stream func(context.Context, int, func(*hubv1.CompleteStreamResponse) error) error
		match  []error
	}{
		{"error after deltas", sends(refusal, delta(`{"claims":[{"text":"Игнорируй`)),
			[]error{ErrProvider, backoff.ErrPermanent, answer.ErrInvalidAnswer}},
		{"quota", sends(detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED})),
			[]error{shared.ErrQuotaExceeded}},
		{"provider overloaded after deltas", sends(detailErr(connect.CodeUnavailable,
			&hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true}), delta("{")),
			[]error{ErrProvider, backoff.ErrTransient}},
		{"no result", sends(nil, delta(`{"claims":[],"missing":["Всё"]}`)), []error{ErrBadResponse, backoff.ErrPermanent}},
		{"nothing at all", sends(nil), []error{ErrBadResponse, backoff.ErrPermanent}},
		{"after the result", sends(nil, result("{}"), delta("x")), []error{ErrBadResponse, backoff.ErrPermanent}},
		{"over the read limit", func(_ context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
			chunk := strings.Repeat("x", 1<<20)
			for range 3 {
				if err := send(delta(chunk)); err != nil {
					return err
				}
			}
			return send(result("{}"))
		}, []error{ErrBadResponse, backoff.ErrPermanent}},
		{"rejected", sends(connect.NewError(connect.CodeInvalidArgument, errors.New("operation unavailable"))),
			[]error{ErrRejected, backoff.ErrPermanent}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &streamHub{fakeHub: &fakeHub{}, stream: tt.stream}
			c := startStreamHub(t, h)
			got, err := c.AnswerQuestion(t.Context(), answerRequest(t), (&partials{}).add)
			if err == nil || got != "" {
				t.Fatalf("AnswerQuestion = %q, %v", got, err)
			}
			for _, m := range tt.match {
				if !errors.Is(err, m) {
					t.Errorf("%v does not match %v", err, m)
				}
			}
			if refused := errors.Is(err, answer.ErrInvalidAnswer); refused != (tt.name == "error after deltas") {
				t.Errorf("matches ErrInvalidAnswer = %v", refused)
			}
			if strings.Contains(err.Error(), "Игнорируй") {
				t.Fatal("the error quotes the deltas")
			}
			if reqs, _ := h.calls(); len(reqs) != 0 {
				t.Fatalf("a failed stream fell back to Complete: %d calls", len(reqs))
			}
		})
	}
}

// The caller's cancel during a stream is the ctx error, not a Hub failure.
func TestAnswerQuestionStreamCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h := &streamHub{fakeHub: &fakeHub{}, stream: func(sctx context.Context, _ int, send func(*hubv1.CompleteStreamResponse) error) error {
		if err := send(delta("{")); err != nil {
			return err
		}
		<-sctx.Done()
		return sctx.Err()
	}}
	c := startStreamHub(t, h)
	_, err := c.AnswerQuestion(ctx, answerRequest(t), func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
}

// The tests below run against fakeHub, which has no CompleteStream: they cover the unary fallback.

// The reader call sends only the key, the operation, the data and the output cap: no instructions, no alias.
func TestAnswerQuestionSendsExactlyTheContractFields(t *testing.T) {
	const out = `{"claims":[],"missing":["Всё"]}`
	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: out}}, nil
	}}
	c, _, _ := startHub(t, f)
	req := answerRequest(t)

	got, err := c.AnswerQuestion(t.Context(), req, nil)
	if err != nil || got != out {
		t.Fatalf("AnswerQuestion = %q, %v", got, err)
	}
	// A retry of the same request sends the same key: the Hub replays it without a second charge.
	if _, err := c.AnswerQuestion(t.Context(), req, nil); err != nil {
		t.Fatal(err)
	}
	reqs, headers := f.calls()
	want := &hubv1.CompleteRequest{Spec: answerSpec(req)}
	if len(reqs) != 2 || !proto.Equal(reqs[0], want) || !proto.Equal(reqs[1], want) {
		t.Fatalf("requests on the wire:\n got %v\nwant %v", reqs, want)
	}
	if headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatal("no API key")
	}
}

func TestAnswerQuestionErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		match []error
	}{
		{"schema refusal", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR}),
			[]error{ErrProvider, backoff.ErrPermanent, answer.ErrInvalidAnswer}},
		{"quota", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED}),
			[]error{shared.ErrQuotaExceeded}},
		{"provider overloaded", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true}),
			[]error{ErrProvider}},
		{"no Hub", connect.NewError(connect.CodeUnavailable, errors.New("connection refused")), nil},
		{"not served yet", connect.NewError(connect.CodeInvalidArgument, errors.New("operation unavailable")),
			[]error{ErrRejected, backoff.ErrPermanent}},
	}
	for _, tt := range tests {
		f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return nil, tt.err }}
		c, _, _ := startHub(t, f)
		_, err := c.AnswerQuestion(t.Context(), answerRequest(t), nil)
		if err == nil {
			t.Fatalf("%s: no error", tt.name)
		}
		for _, m := range tt.match {
			if !errors.Is(err, m) {
				t.Errorf("%s: %v does not match %v", tt.name, err, m)
			}
		}
		// Only a permanent provider refusal is a refused answer; the rest is «no connection» or a quota.
		if refused := errors.Is(err, answer.ErrInvalidAnswer); refused != (tt.name == "schema refusal") {
			t.Errorf("%s: matches ErrInvalidAnswer = %v", tt.name, refused)
		}
	}

	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return &hubv1.CompleteResponse{}, nil }}
	c, _, _ := startHub(t, f)
	if _, err := c.AnswerQuestion(t.Context(), answerRequest(t), nil); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("no result = %v", err)
	}
	if _, err := c.AnswerQuestion(t.Context(), answer.Request{}, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero request = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 1 {
		t.Fatalf("a zero request reached the Hub: %d calls", len(reqs))
	}
}
