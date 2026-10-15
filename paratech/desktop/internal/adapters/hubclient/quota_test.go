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
	"google.golang.org/protobuf/types/known/timestamppb"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// fakeQuota is the public quota method of a local Hub; answer runs on the server goroutine and must not call t.Fatal.
type fakeQuota struct {
	hubv1connect.UnimplementedQuotaServiceHandler

	mu      sync.Mutex
	headers []http.Header
	answer  func(ctx context.Context) (*hubv1.GetRemainingResponse, error)
}

func (f *fakeQuota) GetRemaining(ctx context.Context, r *connect.Request[hubv1.GetRemainingRequest]) (*connect.Response[hubv1.GetRemainingResponse], error) {
	f.mu.Lock()
	f.headers = append(f.headers, r.Header().Clone())
	f.mu.Unlock()
	resp, err := f.answer(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (f *fakeQuota) calls() []http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]http.Header(nil), f.headers...)
}

func amount(v int64) *hubv1.QuotaAmount {
	return &hubv1.QuotaAmount{Unit: hubv1.QuotaUnit_QUOTA_UNIT_TOKENS, Value: v}
}

var periodEnd = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func okQuota(context.Context) (*hubv1.GetRemainingResponse, error) {
	return &hubv1.GetRemainingResponse{Quota: &hubv1.QuotaSnapshot{
		Remaining: amount(600), Reserved: amount(400), Limit: amount(1000), PeriodEndsAt: timestamppb.New(periodEnd),
	}}, nil
}

// startQuotaHub serves f over TLS and returns a client on a URL source, with the API key stored.
func startQuotaHub(t *testing.T, f *fakeQuota, timeout time.Duration) (*Client, *testenv.MemCredentials, *urlSetting, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(hubv1connect.NewQuotaServiceHandler(f))
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	creds := testenv.NewMemCredentials()
	if err := creds.Set(t.Context(), DefaultCredentialName, testKey); err != nil {
		t.Fatal(err)
	}
	urls := &urlSetting{url: srv.URL}
	c, err := New(Config{URLs: urls, HTTPClient: srv.Client(), RequestTimeout: timeout}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return c, creds, urls, srv
}

func TestGetRemaining(t *testing.T) {
	f := &fakeQuota{answer: okQuota}
	c, _, _, _ := startQuotaHub(t, f, 5*time.Second)

	got, err := c.GetRemaining(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tokens := func(v int64) values.QuotaAmount {
		a, err := values.NewQuotaAmount(values.QuotaUnitTokens, v)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	want, err := values.NewQuotaSnapshot(tokens(600), tokens(400), tokens(1000), periodEnd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Remaining() != want.Remaining() || got.Reserved() != want.Reserved() || got.Limit() != want.Limit() {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	if end, ok := got.PeriodEnd(); !ok || !end.Equal(periodEnd) {
		t.Fatalf("period end = %v, %v", end, ok)
	}

	headers := f.calls()
	if len(headers) != 1 || headers[0].Get("Authorization") != "Bearer "+testKey {
		t.Fatalf("calls %d, Authorization %q", len(headers), headers[0].Get("Authorization"))
	}
	for name, vals := range headers[0] {
		for _, v := range vals {
			if name != "Authorization" && strings.Contains(v, testKey) {
				t.Errorf("header %s carries the api key", name)
			}
		}
	}

	// A quota that does not renew has no period end.
	f.answer = func(context.Context) (*hubv1.GetRemainingResponse, error) {
		return &hubv1.GetRemainingResponse{Quota: &hubv1.QuotaSnapshot{Remaining: amount(0), Reserved: amount(0), Limit: amount(0)}}, nil
	}
	got, err = c.GetRemaining(t.Context())
	if _, ok := got.PeriodEnd(); err != nil || ok || got.Limit().Value() != 0 {
		t.Fatalf("snapshot without a period = %+v, %v", got, err)
	}
}

// The error contract is that of Complete (errors.proto): detail first, Connect code as the fallback.
func TestGetRemainingErrors(t *testing.T) {
	invalidKey := detailErr(connect.CodeUnauthenticated, &hubv1.ErrorDetail{
		Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY, Message: "key " + testKey + " is revoked",
	})
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"invalid api key", invalidKey, ErrInvalidAPIKey},
		{"unauthenticated without detail", connect.NewError(connect.CodeUnauthenticated, errors.New(testKey)), ErrInvalidAPIKey},
		{"unavailable", connect.NewError(connect.CodeUnavailable, errors.New("x")), ErrUnavailable},
		{"rate limited", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED, Retryable: true,
		}), ErrRateLimited},
		{"older hub without the method", connect.NewError(connect.CodeUnimplemented, errors.New("x")), ErrRejected},
		{"internal", connect.NewError(connect.CodeInternal, errors.New("x")), ErrRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := startQuotaHub(t, &fakeQuota{answer: func(context.Context) (*hubv1.GetRemainingResponse, error) {
				return nil, tc.err
			}}, 5*time.Second)
			_, err := c.GetRemaining(t.Context())
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if errors.Is(err, shared.ErrNetworkUnavailable) {
				t.Fatal("a Hub answer must not look like a network failure")
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "revoked") {
				t.Fatalf("error text %q carries the key or the Hub's text", err)
			}
		})
	}
}

func TestGetRemainingWithoutTheHub(t *testing.T) {
	c, _, _, srv := startQuotaHub(t, &fakeQuota{answer: okQuota}, 5*time.Second)
	srv.Close()
	_, err := c.GetRemaining(t.Context())
	if !errors.Is(err, shared.ErrNetworkUnavailable) || errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want a network failure", err)
	}
}

// Not configured (no URL, a refused URL, no key) and a key that cannot be sent: nothing reaches the Hub.
func TestGetRemainingNotConfigured(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, u *urlSetting, m *testenv.MemCredentials)
		is    []error
	}{
		{"no url", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) { u.set("", nil) },
			[]error{ErrNotConfigured, ErrNoURL}},
		{"refused url", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) { u.set("http://hub.example.com", nil) },
			[]error{ErrNotConfigured, ErrInvalidConfig}},
		{"no key", func(t *testing.T, _ *urlSetting, m *testenv.MemCredentials) {
			if err := m.Delete(t.Context(), DefaultCredentialName); err != nil {
				t.Fatal(err)
			}
		}, []error{ErrNotConfigured, ErrMissingAPIKey}},
		{"key with a line break", func(t *testing.T, _ *urlSetting, m *testenv.MemCredentials) {
			if err := m.Set(t.Context(), DefaultCredentialName, "abc\r\nX-Evil: 1"); err != nil {
				t.Fatal(err)
			}
		}, []error{ErrMalformedAPIKey}},
		{"store unsupported", func(_ *testing.T, _ *urlSetting, m *testenv.MemCredentials) { m.Fail(errors.ErrUnsupported) },
			[]error{ErrCredentials, errors.ErrUnsupported}},
		{"settings unreadable", func(_ *testing.T, u *urlSetting, _ *testenv.MemCredentials) {
			u.set("", errors.New("database is busy"))
		}, []error{ErrSettings}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeQuota{answer: okQuota}
			c, creds, urls, _ := startQuotaHub(t, f, 5*time.Second)
			tc.setup(t, urls, creds)
			_, err := c.GetRemaining(t.Context())
			for _, w := range tc.is {
				if !errors.Is(err, w) {
					t.Fatalf("error %v does not match %v", err, w)
				}
			}
			if strings.Contains(err.Error(), "X-Evil") {
				t.Fatalf("error text %q carries the key", err)
			}
			if n := len(f.calls()); n != 0 {
				t.Fatalf("hub saw %d requests, want none", n)
			}
		})
	}
}

func TestGetRemainingBadResponses(t *testing.T) {
	snap := func(edit func(*hubv1.QuotaSnapshot)) func(context.Context) (*hubv1.GetRemainingResponse, error) {
		return func(context.Context) (*hubv1.GetRemainingResponse, error) {
			q := &hubv1.QuotaSnapshot{Remaining: amount(600), Reserved: amount(400), Limit: amount(1000)}
			edit(q)
			return &hubv1.GetRemainingResponse{Quota: q}, nil
		}
	}
	cases := []struct {
		name   string
		answer func(context.Context) (*hubv1.GetRemainingResponse, error)
	}{
		{"no snapshot", func(context.Context) (*hubv1.GetRemainingResponse, error) { return &hubv1.GetRemainingResponse{}, nil }},
		{"no remaining", snap(func(q *hubv1.QuotaSnapshot) { q.Remaining = nil })},
		{"no reserved", snap(func(q *hubv1.QuotaSnapshot) { q.Reserved = nil })},
		{"no limit", snap(func(q *hubv1.QuotaSnapshot) { q.Limit = nil })},
		{"negative remaining", snap(func(q *hubv1.QuotaSnapshot) { q.Remaining = amount(-1) })},
		{"negative reserved", snap(func(q *hubv1.QuotaSnapshot) { q.Reserved = amount(-5) })},
		{"remaining above the limit", snap(func(q *hubv1.QuotaSnapshot) { q.Remaining = amount(1001) })},
		{"unspecified unit", snap(func(q *hubv1.QuotaSnapshot) { q.Limit.Unit = hubv1.QuotaUnit_QUOTA_UNIT_UNSPECIFIED })},
		{"unknown unit", snap(func(q *hubv1.QuotaSnapshot) { q.Reserved.Unit = hubv1.QuotaUnit(7) })},
		{"invalid period end", snap(func(q *hubv1.QuotaSnapshot) { q.PeriodEndsAt = &timestamppb.Timestamp{Nanos: -1} })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _, _ := startQuotaHub(t, &fakeQuota{answer: tc.answer}, 5*time.Second)
			_, err := c.GetRemaining(t.Context())
			if !errors.Is(err, ErrBadResponse) {
				t.Fatalf("error = %v, want ErrBadResponse", err)
			}
		})
	}
}

func TestGetRemainingIsBounded(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c, _, _, _ := startQuotaHub(t, &fakeQuota{answer: func(ctx context.Context) (*hubv1.GetRemainingResponse, error) {
		select {
		case <-ctx.Done():
		case <-release:
		}
		return nil, connect.NewError(connect.CodeCanceled, nil)
	}}, 50*time.Millisecond)

	start := time.Now()
	if _, err := c.GetRemaining(t.Context()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("error = %v, want ErrTimeout", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the timeout did not bound the call")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.GetRemaining(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
		t.Fatalf("cancelled: error = %v, want context.Canceled", err)
	}
}

func TestGetRemainingDoesNotFollowARedirect(t *testing.T) {
	var hits int
	var mu sync.Mutex
	other := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
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
	if _, err := c.GetRemaining(t.Context()); err == nil {
		t.Fatal("a redirect answered the call")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatal("the redirect was followed")
	}
}
