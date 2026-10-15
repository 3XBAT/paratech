package connect_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	connectrpc "connectrpc.com/connect"

	"paratech/hub/internal/input/connect"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

type fakeProbe struct{ err error }

func (f fakeProbe) Ping(context.Context) error { return f.err }

func newServer(t *testing.T, probe connect.ReadinessProbe, log *slog.Logger) *httptest.Server {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h, err := connect.NewRouter(connect.Deps{Log: log, Readiness: probe})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func postJSON(t *testing.T, url, body string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestNewRouterRequiresDeps(t *testing.T) {
	if _, err := connect.NewRouter(connect.Deps{Readiness: fakeProbe{}}); err == nil {
		t.Error("no error without a logger")
	}
	if _, err := connect.NewRouter(connect.Deps{Log: slog.New(slog.DiscardHandler)}); err == nil {
		t.Error("no error without a readiness probe")
	}
}

func TestHealthz(t *testing.T) {
	// Liveness does not depend on the database.
	srv := newServer(t, fakeProbe{err: errors.New("down")}, nil)
	if code, body := get(t, srv.URL+connect.PathHealthz); code != http.StatusOK || body != "ok\n" {
		t.Fatalf("healthz = %d %q, want 200 ok", code, body)
	}
}

func TestReadyz(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		srv := newServer(t, fakeProbe{}, nil)
		if code, body := get(t, srv.URL+connect.PathReadyz); code != http.StatusOK || body != "ok\n" {
			t.Fatalf("readyz = %d %q, want 200 ok", code, body)
		}
	})

	t.Run("not ready hides the reason", func(t *testing.T) {
		var logged bytes.Buffer
		srv := newServer(t, fakeProbe{err: errors.New("dial db.internal:5432 for user hub")}, slog.New(slog.NewTextHandler(&logged, nil)))
		code, body := get(t, srv.URL+connect.PathReadyz)
		if code != http.StatusServiceUnavailable || body != "unavailable\n" {
			t.Fatalf("readyz = %d %q, want 503 unavailable", code, body)
		}
		if !strings.Contains(logged.String(), "db.internal") {
			t.Errorf("the reason is not logged: %s", logged.String())
		}
	})
}

func TestConnectRoutes(t *testing.T) {
	srv := newServer(t, fakeProbe{}, nil)

	t.Run("public RPCs answer unimplemented", func(t *testing.T) {
		gw := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
		_, err := gw.Complete(t.Context(), connectrpc.NewRequest(&hubv1.CompleteRequest{}))
		if connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
			t.Fatalf("Complete error = %v, want unimplemented", err)
		}

		q := hubv1connect.NewQuotaServiceClient(srv.Client(), srv.URL)
		_, err = q.GetRemaining(t.Context(), connectrpc.NewRequest(&hubv1.GetRemainingRequest{}))
		if connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
			t.Fatalf("GetRemaining error = %v, want unimplemented", err)
		}

		// The handler, not the router, answered: Connect maps unimplemented to HTTP 501, a missing route is 404.
		if code := postJSON(t, srv.URL+hubv1connect.QuotaServiceGetRemainingProcedure, "{}"); code != http.StatusNotImplemented {
			t.Fatalf("GetRemaining HTTP status = %d, want 501", code)
		}
	})

	t.Run("internal quota RPCs are not routed", func(t *testing.T) {
		for _, p := range []string{
			hubv1connect.QuotaServiceReserveProcedure,
			hubv1connect.QuotaServiceSettleProcedure,
			hubv1connect.QuotaServiceReleaseProcedure,
		} {
			if code := postJSON(t, srv.URL+p, "{}"); code != http.StatusNotFound {
				t.Errorf("%s: HTTP status = %d, want 404", p, code)
			}
		}
	})

	t.Run("message size limit", func(t *testing.T) {
		gw := hubv1connect.NewGatewayServiceClient(srv.Client(), srv.URL)
		complete := func(dataBytes int) error {
			spec := &hubv1.CompletionSpec{Instructions: "x", UntrustedData: strings.Repeat("a", dataBytes)}
			_, err := gw.Complete(t.Context(), connectrpc.NewRequest(&hubv1.CompleteRequest{Spec: spec}))
			return err
		}

		// A request at the 1 MiB payload rule reaches the handler; a grossly oversized one is cut off by the transport.
		if err := complete(1<<20 - 1); connectrpc.CodeOf(err) != connectrpc.CodeUnimplemented {
			t.Fatalf("1 MiB request error = %v, want unimplemented from the handler", err)
		}
		if err := complete(2 << 20); connectrpc.CodeOf(err) != connectrpc.CodeResourceExhausted {
			t.Fatalf("2 MiB request error = %v, want resource_exhausted", err)
		}
	})

	t.Run("unknown path", func(t *testing.T) {
		if code, _ := get(t, srv.URL+"/nope"); code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", code)
		}
	})
}
