package hubclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

func inspectRequest(t *testing.T) inspection.Request {
	t.Helper()
	r, err := inspection.NewRequest("m1", time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		[]inspection.Candidate{{ID: "s1", Name: "Физика"}}, "Лекция 1", 1)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The reader call sends only the key, the operation, the data and the output cap: no instructions, no alias.
func TestInspectMaterialSendsExactlyTheContractFields(t *testing.T) {
	const answer = `{"doc_kind":"lecture"}`
	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) {
		return &hubv1.CompleteResponse{Result: &hubv1.CompletionResult{Output: answer}}, nil
	}}
	c, _, _ := startHub(t, f)
	req := inspectRequest(t)

	out, err := c.InspectMaterial(t.Context(), req)
	if err != nil || out != answer {
		t.Fatalf("InspectMaterial = %q, %v", out, err)
	}
	reqs, headers := f.calls()
	want := &hubv1.CompleteRequest{Spec: &hubv1.CompletionSpec{
		IdempotencyKey:  req.Key(),
		OperationType:   hubv1.OperationType_OPERATION_TYPE_INSPECT_MATERIAL,
		UntrustedData:   req.UntrustedData(),
		MaxOutputTokens: inspection.MaxOutputTokens,
	}}
	if len(reqs) != 1 || !proto.Equal(reqs[0], want) {
		t.Fatalf("request on the wire:\n got %v\nwant %v", reqs, want)
	}
	if headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatal("no API key")
	}
}

func TestInspectMaterialErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		match []error
	}{
		{"schema refusal", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR}),
			[]error{ErrProvider, backoff.ErrPermanent}},
		{"quota", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED}),
			[]error{shared.ErrQuotaExceeded}},
		{"not served yet", connect.NewError(connect.CodeInvalidArgument, errors.New("operation unavailable")),
			[]error{ErrRejected, backoff.ErrPermanent}},
	}
	for _, tt := range tests {
		f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return nil, tt.err }}
		c, _, _ := startHub(t, f)
		_, err := c.InspectMaterial(t.Context(), inspectRequest(t))
		for _, m := range tt.match {
			if !errors.Is(err, m) {
				t.Errorf("%s: %v does not match %v", tt.name, err, m)
			}
		}
	}

	f := &fakeHub{answer: func(context.Context, int) (*hubv1.CompleteResponse, error) { return &hubv1.CompleteResponse{}, nil }}
	c, _, _ := startHub(t, f)
	if _, err := c.InspectMaterial(t.Context(), inspectRequest(t)); !errors.Is(err, ErrBadResponse) {
		t.Fatalf("no result = %v", err)
	}
	if _, err := c.InspectMaterial(t.Context(), inspection.Request{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero request = %v", err)
	}
	if reqs, _ := f.calls(); len(reqs) != 1 {
		t.Fatalf("a zero request reached the Hub: %d calls", len(reqs))
	}
}
