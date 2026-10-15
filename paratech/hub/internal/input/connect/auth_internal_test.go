package connect

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

type countingGateway struct {
	hubv1connect.UnimplementedGatewayServiceHandler
	calls atomic.Int32
}

func (g *countingGateway) Complete(context.Context, *connectrpc.Request[hubv1.CompleteRequest]) (*connectrpc.Response[hubv1.CompleteResponse], error) {
	g.calls.Add(1)
	return connectrpc.NewResponse(&hubv1.CompleteResponse{}), nil
}

func (g *countingGateway) CompleteStream(context.Context, *connectrpc.Request[hubv1.CompleteStreamRequest], *connectrpc.ServerStream[hubv1.CompleteStreamResponse]) error {
	g.calls.Add(1)
	return nil
}

// TestInterceptorFailsClosedWithoutMiddleware: a handler mounted without requireAPIKey still never runs unauthenticated.
func TestInterceptorFailsClosedWithoutMiddleware(t *testing.T) {
	gw := &countingGateway{}
	_, h := hubv1connect.NewGatewayServiceHandler(gw, connectrpc.WithInterceptors(requireIdentity{log: slog.New(slog.DiscardHandler)}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	client := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)

	req := connectrpc.NewRequest(&hubv1.CompleteRequest{})
	req.Header().Set(headerAuthorization, "Bearer any-key")
	_, err := client.Complete(t.Context(), req)
	if connectrpc.CodeOf(err) != connectrpc.CodeUnauthenticated {
		t.Fatalf("unary: err = %v, want unauthenticated", err)
	}

	stream, err := client.CompleteStream(t.Context(), connectrpc.NewRequest(&hubv1.CompleteStreamRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for stream.Receive() {
	}
	if connectrpc.CodeOf(stream.Err()) != connectrpc.CodeUnauthenticated {
		t.Fatalf("stream: err = %v, want unauthenticated", stream.Err())
	}
	_ = stream.Close()

	if n := gw.calls.Load(); n != 0 {
		t.Fatalf("handlers ran %d times without an authenticated key", n)
	}
}

func TestInterceptorLetsAnIdentityThrough(t *testing.T) {
	gw := &countingGateway{}
	_, h := hubv1connect.NewGatewayServiceHandler(gw, connectrpc.WithInterceptors(requireIdentity{log: slog.New(slog.DiscardHandler)}))
	withID := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), apiKeyContextKey{}, shared.APIKeyID("00000000000000aa"))))
	})
	srv := httptest.NewServer(withID)
	t.Cleanup(srv.Close)

	if _, err := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL).Complete(t.Context(), connectrpc.NewRequest(&hubv1.CompleteRequest{})); err != nil {
		t.Fatalf("Complete with an identity: %v", err)
	}
	if n := gw.calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}
