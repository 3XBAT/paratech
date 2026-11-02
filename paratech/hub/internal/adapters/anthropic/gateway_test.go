package anthropic_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paratech/hub/internal/adapters/anthropic"
)

// seen is what a fake gateway got in one request.
type seen struct {
	path, xAPIKey, authorization string
	hasXAPIKey, hasAuthorization bool
}

// gateway starts a fake that records every request and answers with a 400 error, which every call reads the same way.
func gateway(t *testing.T) (*httptest.Server, func() []seen) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []seen
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasX := r.Header["X-Api-Key"]
		_, hasA := r.Header["Authorization"]
		mu.Lock()
		got = append(got, seen{path: r.URL.EscapedPath(), xAPIKey: r.Header.Get("x-api-key"), authorization: r.Header.Get("Authorization"),
			hasXAPIKey: hasX, hasAuthorization: hasA})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"no"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seen {
		mu.Lock()
		defer mu.Unlock()
		return append([]seen(nil), got...)
	}
}

// callAll makes one call of every kind; all of them must fail with the fake's 400.
func callAll(t *testing.T, c *anthropic.Client) {
	t.Helper()
	ctx := t.Context()
	calls := map[string]func(context.Context) error{
		"Complete": func(ctx context.Context) error { _, err := c.Complete(ctx, newRequest(t, "x")); return err },
		"Stream":   func(ctx context.Context) error { _, err := c.Stream(ctx, newRequest(t, "x"), nil); return err },
		"Step":     func(ctx context.Context) error { _, err := c.Step(ctx, agentCall(t)); return err },
		"StepStream": func(ctx context.Context) error {
			_, err := c.StepStream(ctx, agentCall(t), nil)
			return err
		},
	}
	for name, call := range calls {
		err := call(ctx)
		if err == nil {
			t.Fatalf("%s: want the fake's 400", name)
		}
		if strings.Contains(err.Error(), testKey) {
			t.Fatalf("%s: error leaks the key: %v", name, err)
		}
	}
}

func TestAuthModesSendTheKeyInOneHeader(t *testing.T) {
	tests := []struct {
		auth       anthropic.Auth
		wantX      string
		wantBearer string
	}{
		{auth: "", wantX: testKey},
		{auth: anthropic.AuthXAPIKey, wantX: testKey},
		{auth: anthropic.AuthBearer, wantBearer: "Bearer " + testKey},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("auth %q", tt.auth), func(t *testing.T) {
			srv, got := gateway(t)
			c, err := anthropic.New(anthropic.Options{APIKey: testKey, Auth: tt.auth, BaseURL: srv.URL, Timeout: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			callAll(t, c)

			requests := got()
			if len(requests) != 4 {
				t.Fatalf("requests = %d, want 4", len(requests))
			}
			for _, r := range requests {
				if r.xAPIKey != tt.wantX || r.hasXAPIKey != (tt.wantX != "") {
					t.Errorf("x-api-key = %q (present %t), want %q", r.xAPIKey, r.hasXAPIKey, tt.wantX)
				}
				if r.authorization != tt.wantBearer || r.hasAuthorization != (tt.wantBearer != "") {
					t.Errorf("Authorization = %q (present %t), want %q", r.authorization, r.hasAuthorization, tt.wantBearer)
				}
			}
		})
	}
}

func TestBaseURLPathPrefixesTheEndpoint(t *testing.T) {
	for base, want := range map[string]string{
		"":                   "/v1/messages",
		"/":                  "/v1/messages",
		"/anthropic":         "/anthropic/v1/messages",
		"/anthropic/":        "/anthropic/v1/messages",
		"/gw/anthropic.v1-x": "/gw/anthropic.v1-x/v1/messages",
	} {
		t.Run(base, func(t *testing.T) {
			srv, got := gateway(t)
			c, err := anthropic.New(anthropic.Options{APIKey: testKey, Auth: anthropic.AuthBearer, BaseURL: srv.URL + base, Timeout: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(c.String(), srv.URL+want) {
				t.Errorf("client %s does not show the endpoint %s", c, srv.URL+want)
			}
			callAll(t, c)
			for _, r := range got() {
				if r.path != want {
					t.Errorf("path = %q, want %q", r.path, want)
				}
			}
		})
	}
}

func TestBearerRefusesRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okJSON)
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same host, so net/http would keep the Authorization header if it followed.
		http.Redirect(w, r, "/other/v1/messages", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	c, err := anthropic.New(anthropic.Options{APIKey: testKey, Auth: anthropic.AuthBearer, BaseURL: origin.URL + "/gw", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(t.Context(), newRequest(t, "x"))
	pe := providerError(t, err)
	if pe.Status != http.StatusTemporaryRedirect || pe.Reason != "redirect refused" {
		t.Fatalf("got %+v", pe)
	}
	if targetHits.Load() != 0 {
		t.Fatal("the redirect target was called")
	}
}

func TestBearerKeyIsNeverPrinted(t *testing.T) {
	o := anthropic.Options{APIKey: testKey, Auth: anthropic.AuthBearer, BaseURL: "https://gw.example.ru/anthropic", Timeout: time.Minute}
	c, err := anthropic.New(o)
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "options", o, "client", c)
	slog.New(slog.NewTextHandler(&logged, nil)).Info("x", "options", o, "client", c)
	outs := []string{
		fmt.Sprint(o), fmt.Sprintf("%+v", o), fmt.Sprintf("%#v", o), fmt.Sprint(c), fmt.Sprintf("%#v", c),
		fmt.Sprintf("%v", *c), logged.String(),
	}
	for _, out := range outs {
		if strings.Contains(out, testKey) {
			t.Errorf("output leaks the key: %s", out)
		}
		if !strings.Contains(out, "redacted") || !strings.Contains(out, "bearer") {
			t.Errorf("output %s does not show the redacted key and the auth mode", out)
		}
	}

	o.APIKey = testKey + " x"
	if _, err := anthropic.New(o); !errors.Is(err, anthropic.ErrInvalidOptions) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("New with a bad bearer key = %v", err)
	}

	// A key pasted into the auth mode by mistake is not printed either.
	o.APIKey, o.Auth = testKey, anthropic.Auth(testKey)
	logged.Reset()
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "options", o)
	_, err = anthropic.New(o)
	for _, out := range []string{fmt.Sprint(o), fmt.Sprintf("%#v", o), logged.String(), err.Error()} {
		if strings.Contains(out, testKey) {
			t.Errorf("output leaks a misplaced key: %s", out)
		}
	}
}
