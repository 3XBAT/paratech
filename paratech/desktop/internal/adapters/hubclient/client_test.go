package hubclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

const testKey = "la_test_0123456789abcdef"

// fakeHub is a local Hub gateway. It records what reached it; answer decides the reply and runs on the server
// goroutine, so it must not call t.Fatal.
type fakeHub struct {
	hubv1connect.UnimplementedGatewayServiceHandler

	mu      sync.Mutex
	reqs    []*hubv1.CompleteRequest
	headers []http.Header
	answer  func(ctx context.Context, n int) (*hubv1.CompleteResponse, error)
}

func (f *fakeHub) Complete(ctx context.Context, r *connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Msg)
	f.headers = append(f.headers, r.Header().Clone())
	n := len(f.reqs)
	f.mu.Unlock()

	resp, err := f.answer(ctx, n)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (f *fakeHub) calls() ([]*hubv1.CompleteRequest, []http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*hubv1.CompleteRequest(nil), f.reqs...), append([]http.Header(nil), f.headers...)
}

func okAnswer(context.Context, int) (*hubv1.CompleteResponse, error) {
	return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{
		Output: `{"summary":"ok"}`, Usage: &hubv1.Usage{InputTokens: 12, OutputTokens: 3}, ReservationId: "res-1",
	}}, nil
}

// startHub serves f over TLS like the real Hub and returns a client with the API key stored.
func startHub(t *testing.T, f *fakeHub) (*Client, *testenv.MemCredentials, *httptest.Server) {
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
	return c, creds, srv
}

func request(t *testing.T) values.LLMRequest {
	t.Helper()
	req, err := values.NewLLMRequest(values.OperationExtractHomework, "default",
		"Extract homework as JSON.", "Teacher: read chapter 4 by Friday.", "job-42:extract_homework_deadlines")
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func detailErr(code connect.Code, d *hubv1.ErrorDetail) error {
	e := connect.NewError(code, errors.New(d.GetMessage()))
	detail, err := connect.NewErrorDetail(d)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	e.AddDetail(detail)
	return e
}

func TestCompleteSendsExactlyTheContractFields(t *testing.T) {
	f := &fakeHub{answer: okAnswer}
	c, _, _ := startHub(t, f)

	res, err := c.Complete(t.Context(), request(t))
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res.Output.Unvalidated() != `{"summary":"ok"}` || res.InputTokens != 12 || res.OutputTokens != 3 {
		t.Fatalf("result = %q, %d, %d", res.Output.Unvalidated(), res.InputTokens, res.OutputTokens)
	}

	reqs, headers := f.calls()
	if len(reqs) != 1 {
		t.Fatalf("hub saw %d requests, want 1", len(reqs))
	}
	want := &hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: "job-42:extract_homework_deadlines",
		OperationType:  hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES,
		ModelAlias:     "default",
		Instructions:   "Extract homework as JSON.",
		UntrustedData:  "Teacher: read chapter 4 by Friday.",
	}}
	if !proto.Equal(reqs[0], want) {
		t.Fatalf("request on the wire:\n got %v\nwant %v", reqs[0], want)
	}
	if len(reqs[0].ProtoReflect().GetUnknown()) != 0 || len(reqs[0].GetSpec().ProtoReflect().GetUnknown()) != 0 {
		t.Fatal("request carries unknown fields")
	}

	if got := headers[0].Get("Authorization"); got != "Bearer "+testKey {
		t.Fatalf("Authorization = %q", got)
	}
	for name, vals := range headers[0] {
		for _, v := range vals {
			if name != "Authorization" && strings.Contains(v, testKey) {
				t.Errorf("header %s carries the api key", name)
			}
			if strings.Contains(v, "chapter 4") {
				t.Errorf("header %s carries request data", name)
			}
		}
	}
}

func TestOperationsMapToContract(t *testing.T) {
	cases := []struct {
		op   values.Operation
		want hubv1.OperationType
	}{
		{values.OperationSummary, hubv1.OperationType_OPERATION_TYPE_SUMMARY},
		{values.OperationExtractHomework, hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES},
		{values.OperationHomeworkDraft, hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT},
		{values.OperationLectureText, hubv1.OperationType_OPERATION_TYPE_LECTURE_TEXT},
	}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			req, err := values.NewLLMRequest(tc.op, "default", "Do it.", "", "k-1")
			if err != nil {
				t.Fatal(err)
			}
			spec, err := specOf(req)
			if err != nil {
				t.Fatal(err)
			}
			if spec.GetOperationType() != tc.want {
				t.Fatalf("operation type = %v, want %v", spec.GetOperationType(), tc.want)
			}
		})
	}
}

// Test step 3: a retry of the same operation sends the same idempotency key and the same spec, so the Hub replays
// or opens a new attempt of the same key instead of charging twice (AC-11).
func TestRetrySendsSameIdempotencyKey(t *testing.T) {
	f := &fakeHub{answer: func(_ context.Context, n int) (*hubv1.CompleteResponse, error) {
		if n == 1 {
			return nil, detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true, Message: "overloaded",
			})
		}
		return okAnswer(context.Background(), n)
	}}
	c, _, _ := startHub(t, f)
	req := request(t)

	_, err := c.Complete(t.Context(), req)
	if backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("first attempt error %v is not transient", err)
	}
	if _, err := c.Complete(t.Context(), req); err != nil {
		t.Fatalf("retry: %v", err)
	}

	reqs, _ := f.calls()
	if len(reqs) != 2 {
		t.Fatalf("hub saw %d requests, want 2", len(reqs))
	}
	if reqs[0].GetSpec().GetIdempotencyKey() != req.IdempotencyKey() {
		t.Fatalf("key = %q, want the request's %q", reqs[0].GetSpec().GetIdempotencyKey(), req.IdempotencyKey())
	}
	if !proto.Equal(reqs[0], reqs[1]) {
		t.Fatalf("retry differs from the first attempt:\n%v\n%v", reqs[0], reqs[1])
	}
}

// Test step 2 and AC-11/AC-12 error contract, through the generated handler: ErrorDetail first, Connect code as the
// fallback (errors.proto).
func TestErrorMapping(t *testing.T) {
	type want struct {
		is         []error
		class      backoff.Class
		retryAfter time.Duration
	}
	permanentRejected := want{is: []error{ErrRejected}, class: backoff.ClassPermanent}
	cases := []struct {
		name string
		err  error
		want want
	}{
		{"quota exceeded", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: "no quota",
		}), want{is: []error{shared.ErrQuotaExceeded}, class: backoff.ClassTransient}},
		{"rate limited honours retry_after", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Retryable: true, RetryAfter: durationpb.New(7 * time.Second),
		}), want{is: []error{ErrRateLimited, backoff.ErrTransient}, class: backoff.ClassTransient, retryAfter: 7 * time.Second}},
		{"retry_after is capped", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Retryable: true, RetryAfter: durationpb.New(48 * time.Hour),
		}), want{is: []error{ErrRateLimited}, class: backoff.ClassTransient, retryAfter: maxRetryAfter}},
		{"negative retry_after is ignored", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Retryable: true, RetryAfter: durationpb.New(-time.Second),
		}), want{is: []error{ErrRateLimited}, class: backoff.ClassTransient}},
		{"invalid api key", detailErr(connect.CodeUnauthenticated, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY,
		}), want{is: []error{ErrInvalidAPIKey, backoff.ErrPermanent}, class: backoff.ClassPermanent}},
		{"transient provider error", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true, RetryAfter: durationpb.New(1500 * time.Millisecond),
		}), want{is: []error{ErrProvider}, class: backoff.ClassTransient, retryAfter: 1500 * time.Millisecond}},
		{"permanent provider error", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: false,
		}), want{is: []error{ErrProvider}, class: backoff.ClassPermanent}},
		{"idempotency conflict", detailErr(connect.CodeFailedPrecondition, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT,
		}), want{is: []error{ErrIdempotencyConflict}, class: backoff.ClassPermanent}},
		{"unknown detail code falls back on connect code", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode(99),
		}), want{is: []error{ErrUnavailable}, class: backoff.ClassTransient}},
		{"resource exhausted without detail", connect.NewError(connect.CodeResourceExhausted, errors.New("x")),
			want{is: []error{ErrRateLimited}, class: backoff.ClassTransient}},
		{"unauthenticated without detail", connect.NewError(connect.CodeUnauthenticated, errors.New("x")),
			want{is: []error{ErrInvalidAPIKey}, class: backoff.ClassPermanent}},
		{"unavailable without detail", connect.NewError(connect.CodeUnavailable, errors.New("x")),
			want{is: []error{ErrUnavailable}, class: backoff.ClassTransient}},
		{"aborted", connect.NewError(connect.CodeAborted, errors.New("x")),
			want{is: []error{ErrBusy}, class: backoff.ClassTransient}},
		{"invalid argument", connect.NewError(connect.CodeInvalidArgument, errors.New("x")), permanentRejected},
		{"internal", connect.NewError(connect.CodeInternal, errors.New("x")), permanentRejected},
		{"failed precondition without detail", connect.NewError(connect.CodeFailedPrecondition, errors.New("x")), permanentRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := startHub(t, &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
				return nil, tc.err
			}})
			_, err := c.Complete(t.Context(), request(t))
			if err == nil {
				t.Fatal("Complete succeeded")
			}
			for _, w := range tc.want.is {
				if !errors.Is(err, w) {
					t.Errorf("errors.Is(%v, %v) = false", err, w)
				}
			}
			if got := backoff.Classify(err); got != tc.want.class {
				t.Errorf("class = %v, want %v", got, tc.want.class)
			}
			var ra interface{ RetryAfter() time.Duration }
			if !errors.As(err, &ra) || ra.RetryAfter() != tc.want.retryAfter {
				t.Errorf("retry after = %v, want %v", ra, tc.want.retryAfter)
			}
			if errors.Is(err, shared.ErrNetworkUnavailable) {
				t.Error("a Hub answer must not look like a network failure")
			}
			// Every retried Hub failure but the quota wait shows as "service unavailable" (TASK-083).
			wantUnavailable := tc.want.class == backoff.ClassTransient && !errors.Is(err, shared.ErrQuotaExceeded)
			if got := backoff.CauseOf(err) == job.CauseServiceUnavailable; got != wantUnavailable {
				t.Errorf("cause %q, want service unavailable %v", backoff.CauseOf(err), wantUnavailable)
			}
		})
	}
}

// Test step 2 as the job sees it: the quota error parks the job in WAITING_FOR_QUOTA, a network error in
// WAITING_FOR_NETWORK (the port contract of homeworkflow.LLMClient, PRD section 7.2).
func TestQuotaAndNetworkMapToWaitStates(t *testing.T) {
	quotaHub := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return nil, detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: "no quota left",
		})
	}}
	c, _, _ := startHub(t, quotaHub)
	_, err := c.Complete(t.Context(), request(t))
	if got := waitStatus(err); got != "WAITING_FOR_QUOTA" {
		t.Fatalf("quota error %v maps to %q", err, got)
	}
	if errors.Is(err, backoff.ErrPermanent) {
		t.Fatal("a quota wait must not fail the job")
	}

	down, _, srv := startHub(t, &fakeHub{answer: okAnswer})
	srv.Close()
	_, err = down.Complete(t.Context(), request(t))
	if got := waitStatus(err); got != "WAITING_FOR_NETWORK" {
		t.Fatalf("network error %v maps to %q", err, got)
	}
	if backoff.Classify(err) != backoff.ClassTransient {
		t.Fatal("a network error must be transient")
	}
	// The closed listener refuses the dial: the network works and the Hub is down (TASK-083).
	if c := backoff.CauseOf(err); c != job.CauseServiceUnavailable {
		t.Fatalf("refused dial has cause %q, want service unavailable", c)
	}
}

// waitStatus is the mapping the LLM job handler applies to the port errors.
func waitStatus(err error) string {
	switch {
	case errors.Is(err, shared.ErrQuotaExceeded):
		return "WAITING_FOR_QUOTA"
	case errors.Is(err, shared.ErrNetworkUnavailable):
		return "WAITING_FOR_NETWORK"
	}
	return ""
}

func TestErrorTextLeaksNothing(t *testing.T) {
	secretish := "provider said: " + testKey + " Teacher: read chapter 4"
	c, _, _ := startHub(t, &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return nil, detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: secretish,
		})
	}})
	_, err := c.Complete(t.Context(), request(t))
	if err == nil {
		t.Fatal("Complete succeeded")
	}
	for _, s := range []string{testKey, "chapter 4", "provider said", "Extract homework"} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("error text %q contains %q", err.Error(), s)
		}
	}
}

func TestAPIKeyProblemsSendNothing(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(t *testing.T, m *testenv.MemCredentials)
		is     error
		class  backoff.Class
		noText string
	}{
		// Not configured parks the job (TASK-057): a permanent failure would block its key after the key is entered.
		{"missing key", func(t *testing.T, m *testenv.MemCredentials) {
			if err := m.Delete(t.Context(), DefaultCredentialName); err != nil {
				t.Fatal(err)
			}
		}, ErrMissingAPIKey, backoff.ClassTransient, ""},
		{"key with a line break", func(t *testing.T, m *testenv.MemCredentials) {
			if err := m.Set(t.Context(), DefaultCredentialName, "abc\r\nX-Evil: 1"); err != nil {
				t.Fatal(err)
			}
		}, ErrMalformedAPIKey, backoff.ClassPermanent, "X-Evil"},
		{"key with a space", func(t *testing.T, m *testenv.MemCredentials) {
			if err := m.Set(t.Context(), DefaultCredentialName, "abc def"); err != nil {
				t.Fatal(err)
			}
		}, ErrMalformedAPIKey, backoff.ClassPermanent, "abc def"},
		{"store unsupported on this OS", func(_ *testing.T, m *testenv.MemCredentials) {
			m.Fail(errors.ErrUnsupported)
		}, ErrCredentials, backoff.ClassPermanent, ""},
		{"store failure", func(_ *testing.T, m *testenv.MemCredentials) {
			m.Fail(shared.ErrStorage)
		}, ErrCredentials, backoff.ClassTransient, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeHub{answer: okAnswer}
			c, creds, _ := startHub(t, f)
			tc.setup(t, creds)

			_, err := c.Complete(t.Context(), request(t))
			if !errors.Is(err, tc.is) {
				t.Fatalf("error = %v, want %v", err, tc.is)
			}
			if got := backoff.Classify(err); got != tc.class {
				t.Fatalf("class = %v, want %v", got, tc.class)
			}
			if tc.noText != "" && strings.Contains(err.Error(), tc.noText) {
				t.Fatalf("error text %q contains the key", err.Error())
			}
			if reqs, _ := f.calls(); len(reqs) != 0 {
				t.Fatalf("hub saw %d requests, want none", len(reqs))
			}
		})
	}
}

func TestCustomCredentialName(t *testing.T) {
	f := &fakeHub{answer: okAnswer}
	_, creds, srv := startHub(t, f)
	if err := creds.Set(t.Context(), "tester-key", "other_key"); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), CredentialName: "tester-key"}, creds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(t.Context(), request(t)); err != nil {
		t.Fatal(err)
	}
	if _, h := f.calls(); h[0].Get("Authorization") != "Bearer other_key" {
		t.Fatalf("Authorization = %q", h[0].Get("Authorization"))
	}
}

func TestZeroRequestIsRejectedLocally(t *testing.T) {
	f := &fakeHub{answer: okAnswer}
	c, _, _ := startHub(t, f)
	_, err := c.Complete(t.Context(), values.LLMRequest{})
	if !errors.Is(err, ErrInvalidRequest) || backoff.Classify(err) != backoff.ClassPermanent {
		t.Fatalf("error = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 0 {
		t.Fatal("zero request reached the hub")
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f := &fakeHub{answer: func(ctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}
	_, creds, srv := startHub(t, f)
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 50 * time.Millisecond}, creds)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = c.Complete(t.Context(), request(t))
	if !errors.Is(err, ErrTimeout) || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("error = %v, want a transient timeout", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not bound the call")
	}
}

// The Hub's context ends on the Connect-Timeout-Ms we send, which Connect truncates to whole milliseconds, so the Hub
// can give up just before our own timer fires and answer canceled (hub gateway.go). The test shortens the header to
// make that order certain; it is still our timeout, not an unavailable Hub.
func TestHubDeadlineBeforeOursIsATimeout(t *testing.T) {
	f := &fakeHub{answer: func(ctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
		<-ctx.Done()
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Connect-Timeout-Ms") == "" {
			http.Error(w, "no deadline propagated", http.StatusBadRequest)
			return
		}
		r.Header.Set("Connect-Timeout-Ms", "1")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 5 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Complete(t.Context(), request(t))
	if !errors.Is(err, ErrTimeout) || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("error = %v, want a transient timeout", err)
	}
}

func TestParentCancelIsNotClassifiedAsHubFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := &fakeHub{answer: func(rctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
		cancel()
		<-rctx.Done()
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}
	c, _, _ := startHub(t, f)

	_, err := c.Complete(ctx, request(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrTimeout) || errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("cancel misclassified: %v", err)
	}

	if _, err := c.Complete(ctx, request(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled: error = %v", err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var hits int
	var mu sync.Mutex
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	t.Cleanup(other.Close)
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{BaseURL: redirect.URL, HTTPClient: redirect.Client()}, creds)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(t.Context(), request(t))
	if backoff.Classify(err) != backoff.ClassPermanent {
		t.Fatalf("error = %v, want permanent", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatal("redirect was followed")
	}
}

func TestProxyErrorPageIsTransient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway " + testKey + "</html>"))
	}))
	t.Cleanup(srv.Close)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()}, creds)
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.Complete(t.Context(), request(t))
	if !errors.Is(err, ErrUnavailable) || backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("error = %v, want transient unavailable", err)
	}
	if errors.Is(err, shared.ErrNetworkUnavailable) || strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("error = %v", err)
	}
}

func TestBadResponses(t *testing.T) {
	cases := []struct {
		name   string
		answer func(context.Context, int) (*hubv1.CompleteResponse, error)
	}{
		{"no result", func(context.Context, int) (*hubv1.CompleteResponse, error) {
			return &hubv1.CompleteResponse{}, nil
		}},
		{"negative usage", func(context.Context, int) (*hubv1.CompleteResponse, error) {
			return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Usage: &hubv1.Usage{InputTokens: -1}}}, nil
		}},
		{"answer over the read limit", func(context.Context, int) (*hubv1.CompleteResponse, error) {
			return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: strings.Repeat("x", maxResponseBytes+1)}}, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := startHub(t, &fakeHub{answer: tc.answer})
			_, err := c.Complete(t.Context(), request(t))
			if !errors.Is(err, ErrBadResponse) || backoff.Classify(err) != backoff.ClassPermanent {
				t.Fatalf("error = %v, want permanent bad response", err)
			}
		})
	}
}

func TestNewValidatesConfig(t *testing.T) {
	creds := testenv.NewMemCredentials()
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"https", Config{BaseURL: "https://hub.example.com"}, true},
		{"https with path and port", Config{BaseURL: "https://hub.example.com:8443/api/"}, true},
		{"http loopback ipv4", Config{BaseURL: "http://127.0.0.1:8080"}, true},
		{"http loopback ipv6", Config{BaseURL: "http://[::1]:8080"}, true},
		{"http localhost", Config{BaseURL: "http://localhost:8080"}, true},
		{"empty", Config{}, false},
		{"http remote", Config{BaseURL: "http://hub.example.com"}, false},
		{"http lookalike host", Config{BaseURL: "http://127.0.0.1.example.com"}, false},
		{"other scheme", Config{BaseURL: "ftp://hub.example.com"}, false},
		{"no host", Config{BaseURL: "https:///path"}, false},
		{"relative", Config{BaseURL: "hub.example.com"}, false},
		{"userinfo", Config{BaseURL: "https://user:pw@hub.example.com"}, false},
		{"query", Config{BaseURL: "https://hub.example.com/?x=1"}, false},
		{"fragment", Config{BaseURL: "https://hub.example.com/#x"}, false},
		{"negative timeout", Config{BaseURL: "https://hub.example.com", RequestTimeout: -time.Second}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := New(tc.cfg, creds)
			if tc.ok != (err == nil) {
				t.Fatalf("New error = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error %v does not wrap ErrInvalidConfig", err)
			}
			if err == nil && (c.timeout != DefaultRequestTimeout || c.credName != DefaultCredentialName) {
				t.Fatalf("defaults not applied: %v %q", c.timeout, c.credName)
			}
		})
	}
	if _, err := New(Config{BaseURL: "https://hub.example.com"}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil credentials: error = %v", err)
	}
}
