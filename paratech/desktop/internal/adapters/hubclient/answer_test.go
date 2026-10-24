package hubclient

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

func answerRequest(t *testing.T) answer.Request {
	t.Helper()
	r, err := answer.NewRequest("Что такое производная?", []answer.Excerpt{{
		Text:   "Производная — предел отношения приращений.",
		Source: answer.Source{Recording: "rec1", Section: answer.SectionLectureText, StartMs: 1000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The reader call sends only the key, the operation, the data and the output cap: no instructions, no alias.
func TestAnswerQuestionSendsExactlyTheContractFields(t *testing.T) {
	const out = `{"claims":[],"missing":["Всё"]}`
	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: out}}, nil
	}}
	c, _, _ := startHub(t, f)
	req := answerRequest(t)

	got, err := c.AnswerQuestion(t.Context(), req)
	if err != nil || got != out {
		t.Fatalf("AnswerQuestion = %q, %v", got, err)
	}
	// A retry of the same request sends the same key: the Hub replays it without a second charge.
	if _, err := c.AnswerQuestion(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	reqs, headers := f.calls()
	want := &hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_ANSWER,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: answer.MaxOutputTokens,
	}}
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
		_, err := c.AnswerQuestion(t.Context(), answerRequest(t))
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
	if _, err := c.AnswerQuestion(t.Context(), answerRequest(t)); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("no result = %v", err)
	}
	if _, err := c.AnswerQuestion(t.Context(), answer.Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero request = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 1 {
		t.Fatalf("a zero request reached the Hub: %d calls", len(reqs))
	}
}
