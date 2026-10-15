package connect_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/connect"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// unreadBody fails the request if anything reads it.
type unreadBody struct{ reads atomic.Int32 }

func (b *unreadBody) Read([]byte) (int, error) {
	b.reads.Add(1)
	return 0, errors.New("the body must not be read before authentication")
}

func (*unreadBody) Close() error { return nil }

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestAuthRejectsBeforeReadingTheBody: a rejected request is answered before its body is touched, in every protocol.
func TestAuthRejectsBeforeReadingTheBody(t *testing.T) {
	k := newKeys(t)
	gw := &recordingGateway{}
	h, err := connect.NewRouter(connect.Deps{Log: discard(), Readiness: fakeProbe{}, Auth: keyAuth(t, k.store), Gateway: gw})
	if err != nil {
		t.Fatal(err)
	}

	protocols := []struct {
		name        string
		contentType string
		connectV1   bool
		grpcStatus  bool // the code is in grpc-status, not in the HTTP status
	}{
		{"connect proto", "application/proto", true, false},
		{"connect json", "application/json", true, false},
		{"connect stream", "application/connect+proto", true, false},
		{"grpc", "application/grpc", false, true},
		{"grpc-web", "application/grpc-web+proto", false, true},
		{"not an RPC protocol", "text/plain", false, false},
	}
	procedures := []string{
		hubv1connect.GatewayServiceCompleteProcedure,
		hubv1connect.GatewayServiceCompleteStreamProcedure,
		hubv1connect.QuotaServiceGetRemainingProcedure,
	}
	for _, p := range protocols {
		for _, procedure := range procedures {
			for name, authorization := range map[string]string{"no key": "", "unknown key": "Bearer " + k.foreign.Reveal()} {
				t.Run(p.name+procedure+"/"+name, func(t *testing.T) {
					body := &unreadBody{}
					req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, procedure, nil)
					req.Body = body
					req.Header.Set("Content-Type", p.contentType)
					if p.connectV1 {
						req.Header.Set("Connect-Protocol-Version", "1")
					}
					if authorization != "" {
						req.Header.Set("Authorization", authorization)
					}
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					res := rec.Result()

					if n := body.reads.Load(); n != 0 {
						t.Fatalf("the body was read %d times before the request was rejected", n)
					}
					switch {
					case p.grpcStatus:
						status := res.Trailer.Get("Grpc-Status") + res.Header.Get("Grpc-Status")
						if status != "16" || res.Trailer.Get("Grpc-Status-Details-Bin")+res.Header.Get("Grpc-Status-Details-Bin") == "" {
							t.Fatalf("grpc-status %q with details %v %v, want 16 (unauthenticated) with the error detail", status, res.Header, res.Trailer)
						}
					case p.contentType == "application/connect+proto":
						if res.StatusCode != http.StatusOK || rec.Body.Len() == 0 {
							t.Fatalf("stream answer = %d with %d bytes, want 200 with an end-of-stream error", res.StatusCode, rec.Body.Len())
						}
					default:
						if res.StatusCode != http.StatusUnauthorized {
							t.Fatalf("HTTP status = %d, want 401", res.StatusCode)
						}
					}
				})
			}
		}
	}
	if calls := gw.calls(); len(calls) != 0 {
		t.Fatalf("handlers ran for rejected keys: %v", calls)
	}
}

// TestAuthSameAnswerInEveryProtocol: Connect, gRPC and gRPC-Web clients get the same code, message and detail.
func TestAuthSameAnswerInEveryProtocol(t *testing.T) {
	k := newKeys(t)
	h, err := connect.NewRouter(connect.Deps{Log: discard(), Readiness: fakeProbe{}, Auth: keyAuth(t, k.store)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h) // gRPC needs HTTP/2
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	protocols := map[string][]connectrpc.ClientOption{
		"connect":  nil,
		"grpc":     {connectrpc.WithGRPC()},
		"grpc-web": {connectrpc.WithGRPCWeb()},
	}
	var want string
	for name, opts := range protocols {
		for _, r := range protectedRPCs {
			for _, authorization := range [][]string{nil, {"Bearer " + k.revoked.Reveal()}} {
				got := assertRejected(t, r.call(t.Context(), srv, opts, authorization))
				if want == "" {
					want = got
				}
				if got != want {
					t.Fatalf("%s/%s: answer %q differs from %q", name, r.name, got, want)
				}
			}
		}
	}

	// A valid key passes in every protocol.
	for name, opts := range protocols {
		for _, r := range protectedRPCs {
			if err := r.call(t.Context(), srv, opts, []string{"Bearer " + k.valid.Reveal()}); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
				t.Errorf("%s/%s with a valid key: err = %v, want unimplemented from the handler", name, r.name, err)
			}
		}
	}
}

// blockingAuth is a key store that hangs until the call is cancelled.
type blockingAuth struct{ deadline atomic.Bool }

func (b *blockingAuth) Authenticate(ctx context.Context, _ string) (shared.APIKeyID, error) {
	_, ok := ctx.Deadline()
	b.deadline.Store(ok)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestAuthHangingStoreIsUnavailable(t *testing.T) {
	auth := &blockingAuth{}
	srv := authServer(t, auth, nil, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second) // far above the Hub's own bound
	defer cancel()
	err := protectedRPCs[0].call(ctx, srv, nil, []string{"Bearer " + anyKey})
	var ce *connectrpc.Error
	if !errors.As(err, &ce) || ce.Code() != connectrpc.CodeUnavailable || len(ce.Details()) != 0 {
		t.Fatalf("err = %v, want unavailable without detail", err)
	}
	if ctx.Err() != nil {
		t.Fatal("the client deadline expired: the store lookup is not bounded")
	}
	if !auth.deadline.Load() {
		t.Fatal("the store lookup ran without a deadline")
	}
}

func TestAuthLogsRejectionsAtInfo(t *testing.T) {
	k := newKeys(t)
	var logged bytes.Buffer
	srv := authServer(t, keyAuth(t, k.store), nil, slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo})))

	for _, authorization := range [][]string{nil, {"Bearer " + k.revoked.Reveal()}} {
		assertRejected(t, protectedRPCs[0].call(t.Context(), srv, nil, authorization))
	}
	for _, want := range []string{"level=INFO", "missing or malformed Authorization header", "revoked", hubv1connect.GatewayServiceCompleteProcedure} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("no %q in the log:\n%s", want, logged.String())
		}
	}
	if strings.Contains(logged.String(), k.revoked.ID().String()) {
		t.Fatal("the log holds the key id")
	}
}
