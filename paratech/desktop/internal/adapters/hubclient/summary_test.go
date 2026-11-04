package hubclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

func summaryRequest(t *testing.T, previous string, pairs ...agent.SummaryPair) agent.SummaryRequest {
	t.Helper()
	var prev agent.HistorySummary
	if previous != "" {
		var err error
		if prev, err = agent.ParseHistorySummary(2, previous); err != nil {
			t.Fatal(err)
		}
	}
	if len(pairs) == 0 {
		pairs = []agent.SummaryPair{{Student: "что задали по физике", Reply: "reply: задача 5"}}
	}
	req, err := agent.NewSummaryRequest("th1", prev, 12, pairs)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func summaryAnswer(output string) func(context.Context, int) (*hubv1.CompleteResponse, error) {
	return func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: output}}, nil
	}
}

// The call sends only the key, the operation and the data: no instructions, no alias, no output cap.
func TestSummarizeSendsExactlyTheContractFields(t *testing.T) {
	f := &fakeHub{answer: summaryAnswer(`{"summary":"Обсуждали задачу 5 по физике."}`)}
	c, _, _ := startHub(t, f)
	req := summaryRequest(t, "сводка просьб 1–2",
		agent.SummaryPair{Student: "задача 5\nи 6", Reply: "reply: решение"},
		agent.SummaryPair{Student: "а <b>&</b>?"})

	got, err := c.Summarize(t.Context(), req)
	if err != nil || got != "Обсуждали задачу 5 по физике." {
		t.Fatalf("Summarize = %q, %v", got, err)
	}
	reqs, headers := f.calls()
	data := `{"summary":"сводка просьб 1–2","pairs":[{"student":"задача 5\nи 6","reply":"reply: решение"},` +
		`{"student":"а <b>&</b>?","reply":""}]}`
	sum := sha256.Sum256([]byte(data))
	want := &hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey: "cs:th1:12:" + hex.EncodeToString(sum[:4]), // the hash of exactly the data sent
		OperationType:  hubv1.OperationType_OPERATION_TYPE_CHAT_SUMMARY,
		UntrustedData:  data,
	}}
	if len(reqs) != 1 || !proto.Equal(reqs[0], want) {
		t.Fatalf("request on the wire:\n got %v\nwant %v", reqs, want)
	}
	if headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatal("no API key")
	}
}

// The data the adapter sends has exactly the size the domain checked against the Hub limit, escapes included.
func TestSummaryDataSizeMatchesTheDomain(t *testing.T) {
	escapes := `"\<>&` + "\n\t" + string(rune(0x2028)) + "я"
	req := summaryRequest(t, `сводка "в кавычках"`,
		agent.SummaryPair{Student: strings.Repeat(escapes, 200), Reply: strings.Repeat(`\"`, 300)},
		agent.SummaryPair{Student: "без экранов", Reply: "<html>"})
	if got := summaryData(req); len(got) != req.EncodedSize() {
		t.Fatalf("data of %d bytes, the domain counted %d", len(got), req.EncodedSize())
	}
	// The key hashes the same bytes, so the same data is the same key on the Hub.
	sum := sha256.Sum256([]byte(summaryData(req)))
	if want := "cs:th1:12:" + hex.EncodeToString(sum[:4]); req.Key() != want {
		t.Fatalf("key %q, want %q", req.Key(), want)
	}
	if got := summaryData(summaryRequest(t, "")); !strings.HasPrefix(got, `{"summary":"","pairs":[`) {
		t.Fatalf("without a previous summary: %s", got)
	}
}

func TestSummarizeReadsTheAnswer(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string // "" refused
	}{
		{"canonical", `{"summary":"Итог: задача 5 решена."}`, "Итог: задача 5 решена."},
		{"with escapes", `{"summary":"строка 1\nстрока \"2\""}`, "строка 1\nстрока \"2\""},
		{"surrounding whitespace", " {\"summary\":\"a\"}\n", "a"},
		{"extra key", `{"summary":"a","note":"b"}`, ""},
		{"extra key first", `{"note":"b","summary":"a"}`, ""},
		{"repeated key", `{"summary":"a","summary":"b"}`, ""},
		{"repeated key, same text", `{"summary":"a","summary":"a"}`, ""},
		{"repeated key, other case", `{"summary":"a","Summary":"b"}`, ""},
		{"object value", `{"summary":{"text":"a"}}`, ""},
		{"array value", `{"summary":["a"]}`, ""},
		{"unterminated", `{"summary":"a"`, ""},
		{"trailing comma", `{"summary":"a",}`, ""},
		{"other case", `{"Summary":"a"}`, ""},
		{"not a string", `{"summary":5}`, ""},
		{"null", `{"summary":null}`, ""},
		{"empty object", `{}`, ""},
		{"trailing data", `{"summary":"a"}{"summary":"b"}`, ""},
		{"trailing garbage", `{"summary":"a"} ok`, ""},
		{"prose", `Вот сводка: всё хорошо.`, ""},
		{"fenced", "```json\n{\"summary\":\"a\"}\n```", ""},
		{"empty", ``, ""},
		{"array", `["a"]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := startHub(t, &fakeHub{answer: summaryAnswer(tt.output)})
			got, err := c.Summarize(t.Context(), summaryRequest(t, ""))
			if tt.want != "" {
				if err != nil || got != tt.want {
					t.Fatalf("Summarize = %q, %v", got, err)
				}
				return
			}
			if got != "" || !errors.Is(err, ErrBadResponse) || !errors.Is(err, backoff.ErrPermanent) ||
				!errors.Is(err, agent.ErrSummaryRefused) {
				t.Fatalf("Summarize = %q, %v; want a refused answer", got, err)
			}
		})
	}

	c, _, _ := startHub(t, &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{}, nil
	}})
	if _, err := c.Summarize(t.Context(), summaryRequest(t, "")); !errors.Is(err, ErrBadResponse) ||
		errors.Is(err, agent.ErrSummaryRefused) {
		t.Fatalf("no result = %v", err)
	}
}

func TestSummarizeErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		match   []error
		refused bool
	}{
		{"refused answer", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: hubMsgModelAnswer,
		}), []error{ErrProvider, ErrModelAnswer, backoff.ErrPermanent}, true},
		{"provider refused", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR}),
			[]error{ErrProvider, backoff.ErrPermanent}, true},
		{"provider down", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true,
		}), []error{ErrProvider, backoff.ErrTransient, shared.ErrServiceUnavailable}, false},
		{"quota", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED}),
			[]error{shared.ErrQuotaExceeded}, false},
		{"rate limit", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Retryable: true,
		}), []error{ErrRateLimited, backoff.ErrTransient}, false},
		{"invalid key", detailErr(connect.CodeUnauthenticated, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY}),
			[]error{ErrInvalidAPIKey, backoff.ErrPermanent}, false},
		{"key conflict", detailErr(connect.CodeFailedPrecondition, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}),
			[]error{ErrIdempotencyConflict, backoff.ErrPermanent}, false},
		{"hub unavailable", connect.NewError(connect.CodeUnavailable, errors.New("x")),
			[]error{ErrUnavailable, backoff.ErrTransient}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := startHub(t, &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return nil, tt.err }})
			_, err := c.Summarize(t.Context(), summaryRequest(t, ""))
			for _, m := range tt.match {
				if !errors.Is(err, m) {
					t.Errorf("%v does not match %v", err, m)
				}
			}
			if got := errors.Is(err, agent.ErrSummaryRefused); got != tt.refused {
				t.Errorf("%v matches ErrSummaryRefused = %v", err, got)
			}
		})
	}
}

func TestSummarizeLocalFailures(t *testing.T) {
	f := &fakeHub{answer: summaryAnswer(`{"summary":"a"}`)}
	c, _, srv := startHub(t, f)
	if _, err := c.Summarize(t.Context(), agent.SummaryRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero request = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Summarize(ctx, summaryRequest(t, "")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 0 {
		t.Fatalf("%d calls reached the Hub", len(reqs))
	}

	unset, err := New(Config{BaseURL: "https://hub.invalid"}, testenv.NewMemCredentials())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unset.Summarize(t.Context(), summaryRequest(t, "")); !errors.Is(err, ErrNotConfigured) ||
		!errors.Is(err, shared.ErrNetworkUnavailable) {
		t.Fatalf("without a key = %v", err)
	}

	srv.Close()
	if _, err := c.Summarize(t.Context(), summaryRequest(t, "")); !errors.Is(err, shared.ErrNetworkUnavailable) ||
		errors.Is(err, agent.ErrSummaryRefused) {
		t.Fatalf("Hub gone = %v", err)
	}
}

// The call is bounded by RequestTimeout; the caller going away is its own error, not the Hub's.
func TestSummarizeTimeoutAndCancel(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	slow := &fakeHub{answer: func(ctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}
	_, creds, srv := startHub(t, slow)
	c, err := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client(), RequestTimeout: 50 * time.Millisecond}, creds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Summarize(t.Context(), summaryRequest(t, "")); !errors.Is(err, ErrTimeout) ||
		backoff.Classify(err) != backoff.ClassTransient {
		t.Fatalf("slow Hub = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	gone := &fakeHub{answer: func(rctx context.Context, _ int) (*hubv1.CompleteResponse, error) {
		cancel()
		<-rctx.Done()
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}
	c2, _, _ := startHub(t, gone)
	if _, err := c2.Summarize(ctx, summaryRequest(t, "")); !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("caller gone = %v", err)
	}
}

// Neither the key, the student's text nor the Hub's message reaches the error text.
func TestSummarizeErrorTextLeaksNothing(t *testing.T) {
	secretish := "provider said: " + testKey + " что задали по физике"
	for _, answer := range []func(context.Context, int) (*hubv1.CompleteResponse, error){
		func(context.Context, int) (*hubv1.CompleteResponse, error) {
			return nil, detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
				Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: secretish,
			})
		},
		summaryAnswer(`{"summary":"` + secretish + `","x":1}`),
	} {
		c, _, _ := startHub(t, &fakeHub{answer: answer})
		_, err := c.Summarize(t.Context(), summaryRequest(t, ""))
		if err == nil {
			t.Fatal("Summarize succeeded")
		}
		for _, s := range []string{testKey, "физике", "provider said", "задача 5"} {
			if strings.Contains(err.Error(), s) {
				t.Fatalf("error text %q contains %q", err.Error(), s)
			}
		}
	}
}
