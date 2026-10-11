package shared_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// The doc comments of errors.proto promise that the Hub attaches an ErrorDetail to the Connect error and that the client
// reads it back. These tests prove it through the generated handler and client over a real HTTP round trip, in both wire
// codecs (binary protobuf and the JSON mode used for curl debugging), for a unary call and for a stream that fails after
// a delta was delivered.

// fakeGateway is a minimal GatewayServiceHandler whose behaviour is set by the test.
type fakeGateway struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	complete       func(*connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error)
	completeStream func(*connect.ServerStream[hubv1.CompleteStreamResponse]) error
}

func (f *fakeGateway) Complete(_ context.Context, r *connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error) {
	return f.complete(r)
}

func (f *fakeGateway) CompleteStream(_ context.Context, _ *connect.Request[hubv1.CompleteStreamRequest], s *connect.ServerStream[hubv1.CompleteStreamResponse]) error {
	return f.completeStream(s)
}

// detailError is what a Hub handler returns: a Connect error carrying an ErrorDetail. It runs on the server goroutine,
// so it must not call t.Fatal.
func detailError(t *testing.T, code connect.Code, d *hubv1.ErrorDetail) error {
	t.Helper()
	detail, err := connect.NewErrorDetail(d)
	if err != nil {
		t.Errorf("NewErrorDetail: %v", err)
		return connect.NewError(connect.CodeInternal, err)
	}
	e := connect.NewError(code, errors.New(d.GetMessage()))
	e.AddDetail(detail)
	return e
}

// readDetail extracts the single ErrorDetail from an error returned by a generated client.
func readDetail(t *testing.T, err error, wantCode connect.Code) *hubv1.ErrorDetail {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error is %T (%v), want *connect.Error", err, err)
	}
	if ce.Code() != wantCode {
		t.Fatalf("connect code is %v, want %v", ce.Code(), wantCode)
	}
	if len(ce.Details()) != 1 {
		t.Fatalf("got %d details, want 1", len(ce.Details()))
	}
	v, err := ce.Details()[0].Value()
	if err != nil {
		t.Fatalf("detail Value: %v", err)
	}
	d, ok := v.(*hubv1.ErrorDetail)
	if !ok {
		t.Fatalf("detail is %T, want *hubv1.ErrorDetail", v)
	}
	return d
}

func codecs() map[string][]connect.ClientOption {
	return map[string][]connect.ClientOption{
		"protobuf": nil,
		"json":     {connect.WithProtoJSON()},
	}
}

func startGateway(t *testing.T, f *fakeGateway) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewGatewayServiceHandler(f))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func spec() *hubv1.CompletionSpec {
	return &hubv1.CompletionSpec{
		IdempotencyKey: "job-1", OperationType: hubv1.OperationType_OPERATION_TYPE_SUMMARY,
		ModelAlias: "default", Instructions: "Summarize.", UntrustedData: "text",
	}
}

func TestUnaryErrorDetailSurvivesConnectRoundTrip(t *testing.T) {
	period := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	snapshot := &hubv1.QuotaSnapshot{
		Remaining:    &hubv1.QuotaAmount{Unit: hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, Value: 120},
		Reserved:     &hubv1.QuotaAmount{Unit: hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, Value: 30},
		Limit:        &hubv1.QuotaAmount{Unit: hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, Value: 1000},
		PeriodEndsAt: timestamppb.New(period),
	}
	cases := []struct {
		name string
		code connect.Code
		sent *hubv1.ErrorDetail
	}{
		{"rate limited with retry_after", connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, RetryAfter: durationpb.New(7 * time.Second),
			Message: "slow down", Retryable: true,
		}},
		{"quota exceeded with snapshot", connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED, Message: "no quota left", Quota: snapshot,
		}},
		{"permanent provider error", connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "the request was refused", Retryable: false,
		}},
		{"transient provider error", connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "provider overloaded", Retryable: true,
			RetryAfter: durationpb.New(1500 * time.Millisecond),
		}},
		{"invalid api key", connect.CodeUnauthenticated, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY, Message: "unknown key",
		}},
		{"idempotency conflict", connect.CodeFailedPrecondition, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, Message: "key reused with another request",
		}},
	}
	for codecName, opts := range codecs() {
		for _, tc := range cases {
			t.Run(codecName+"/"+tc.name, func(t *testing.T) {
				srv := startGateway(t, &fakeGateway{
					complete: func(r *connect.Request[hubv1.CompleteRequest]) (*connect.Response[hubv1.CompleteResponse], error) {
						if r.Msg.GetSpec().GetIdempotencyKey() != "job-1" {
							t.Errorf("handler saw idempotency key %q, want job-1", r.Msg.GetSpec().GetIdempotencyKey())
						}
						return nil, detailError(t, tc.code, tc.sent)
					},
				})
				client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...)
				_, err := client.Complete(t.Context(), connect.NewRequest(&hubv1.CompleteRequest{Spec: spec()}))
				if err == nil {
					t.Fatal("Complete succeeded, want an error")
				}
				got := readDetail(t, err, tc.code)
				if !proto.Equal(got, tc.sent) {
					t.Fatalf("detail changed in transit:\n got %v\nwant %v", got, tc.sent)
				}
			})
		}
	}
}

func TestStreamFailingAfterDeltaKeepsDeltaAndErrorDetail(t *testing.T) {
	sent := &hubv1.ErrorDetail{
		Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Message: "provider failed mid-stream",
		RetryAfter: durationpb.New(2 * time.Second), Retryable: true,
	}
	for codecName, opts := range codecs() {
		t.Run(codecName, func(t *testing.T) {
			srv := startGateway(t, &fakeGateway{
				completeStream: func(s *connect.ServerStream[hubv1.CompleteStreamResponse]) error {
					if err := s.Send(&hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Delta{Delta: "Hel"}}); err != nil {
						return err
					}
					return detailError(t, connect.CodeUnavailable, sent)
				},
			})
			client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL, opts...)
			stream, err := client.CompleteStream(t.Context(), connect.NewRequest(&hubv1.CompleteStreamRequest{Spec: spec()}))
			if err != nil {
				t.Fatalf("CompleteStream: %v", err)
			}
			defer func() { _ = stream.Close() }()

			if !stream.Receive() {
				t.Fatalf("no first message, stream error: %v", stream.Err())
			}
			if got := stream.Msg().GetDelta(); got != "Hel" {
				t.Fatalf("first delta is %q, want %q", got, "Hel")
			}
			if stream.Receive() {
				t.Fatalf("got a second message %v, want the stream to fail", stream.Msg())
			}
			got := readDetail(t, stream.Err(), connect.CodeUnavailable)
			if !proto.Equal(got, sent) {
				t.Fatalf("detail changed in transit:\n got %v\nwant %v", got, sent)
			}
		})
	}
}

// A successful stream: deltas, then exactly one result; the result carries the fields of CompletionResult intact.
func TestStreamSuccessDeliversDeltasThenResult(t *testing.T) {
	result := &hubv1.CompletionResult{
		Output: "Hello", Usage: &hubv1.Usage{InputTokens: 10, OutputTokens: 2}, ReservationId: "res-1", Replayed: true,
	}
	srv := startGateway(t, &fakeGateway{
		completeStream: func(s *connect.ServerStream[hubv1.CompleteStreamResponse]) error {
			for _, d := range []string{"Hel", "lo"} {
				if err := s.Send(&hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Delta{Delta: d}}); err != nil {
					return err
				}
			}
			return s.Send(&hubv1.CompleteStreamResponse{Event: &hubv1.CompleteStreamResponse_Result{Result: result}})
		},
	})
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
	stream, err := client.CompleteStream(t.Context(), connect.NewRequest(&hubv1.CompleteStreamRequest{Spec: spec()}))
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var deltas string
	var got *hubv1.CompletionResult
	for stream.Receive() {
		switch ev := stream.Msg().GetEvent().(type) {
		case *hubv1.CompleteStreamResponse_Delta:
			if got != nil {
				t.Fatal("delta after the result")
			}
			deltas += ev.Delta
		case *hubv1.CompleteStreamResponse_Result:
			got = ev.Result
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if got == nil || !proto.Equal(got, result) || deltas != got.GetOutput() {
		t.Fatalf("result %v with deltas %q, want %v and deltas equal to its output", got, deltas, result)
	}
}
