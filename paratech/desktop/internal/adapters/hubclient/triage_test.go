package hubclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

func triageRequest(t *testing.T) work.Request {
	t.Helper()
	w, err := work.New("w1", work.Source{Assignment: "a1"}, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.Request("Эссе на 3 страницы", "")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The reader call sends only the key, the operation, the data and the output cap: no instructions, no alias.
func TestTriageAssignmentSendsExactlyTheContractFields(t *testing.T) {
	const answer = `{"work_type":"essay"}`
	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: answer}}, nil
	}}
	c, _, _ := startHub(t, f)
	req := triageRequest(t)

	out, err := c.TriageAssignment(t.Context(), req)
	if err != nil || out != answer {
		t.Fatalf("TriageAssignment = %q, %v", out, err)
	}
	reqs, headers := f.calls()
	want := &hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_TRIAGE_ASSIGNMENT,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: work.MaxOutputTokens,
	}}
	if len(reqs) != 1 || !proto.Equal(reqs[0], want) {
		t.Fatalf("request on the wire:\n got %v\nwant %v", reqs, want)
	}
	if headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatal("no API key")
	}
}

func TestTriageAssignmentErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		match []error
	}{
		{"schema refusal", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR}),
			[]error{ErrProvider, backoff.ErrPermanent, work.ErrInvalidAnswer}},
		{"quota", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED}),
			[]error{shared.ErrQuotaExceeded}},
		{"not served yet", connect.NewError(connect.CodeInvalidArgument, errors.New("operation unavailable")),
			[]error{ErrRejected, backoff.ErrPermanent}},
	}
	for _, tt := range tests {
		f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return nil, tt.err }}
		c, _, _ := startHub(t, f)
		_, err := c.TriageAssignment(t.Context(), triageRequest(t))
		for _, m := range tt.match {
			if !errors.Is(err, m) {
				t.Errorf("%s: %v does not match %v", tt.name, err, m)
			}
		}
		// Only the schema refusal is a refused answer; a rejected request or a quota is not charged as one.
		if refused := errors.Is(err, work.ErrInvalidAnswer); refused != (tt.name == "schema refusal") {
			t.Errorf("%s: matches ErrInvalidAnswer = %v", tt.name, refused)
		}
	}

	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return &hubv1.CompleteResponse{}, nil }}
	c, _, _ := startHub(t, f)
	if _, err := c.TriageAssignment(t.Context(), triageRequest(t)); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("no result = %v", err)
	}
	if _, err := c.TriageAssignment(t.Context(), work.Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero request = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 1 {
		t.Fatalf("a zero request reached the Hub: %d calls", len(reqs))
	}
}
