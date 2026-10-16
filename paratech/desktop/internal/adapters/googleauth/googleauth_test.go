package googleauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// rig is an Authorizer against the fake Google with the fake client stored.
type rig struct {
	a       *Authorizer
	google  *testenv.FakeGoogleOAuth
	creds   *testenv.MemCredentials
	browser *testenv.FakeBrowser
}

func newRig(t *testing.T, opts ...Option) *rig {
	t.Helper()
	ctx := context.Background()
	g := testenv.StartFakeGoogleOAuth(t)
	creds := testenv.NewMemCredentials()
	for name, v := range map[string]string{
		ClientIDName: testenv.FakeGoogleClientID, ClientSecretName: testenv.FakeGoogleClientSecret,
	} {
		if err := creds.Set(ctx, name, v); err != nil {
			t.Fatal(err)
		}
	}
	b := testenv.NewFakeBrowser()
	all := append([]Option{
		WithEndpoints(g.AuthURL(), g.TokenURL(), g.RevokeURL()), WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		WithSignInTimeout(10 * time.Second),
	}, opts...)
	a, err := New(creds, b, all...)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{a: a, google: g, creds: creds, browser: b}
}

// stored returns the secret under name, or "".
func (r *rig) stored(t *testing.T, name string) string {
	t.Helper()
	v, err := r.creds.Get(context.Background(), name)
	if errors.Is(err, shared.ErrNotFound) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// redirectHost is the host:port of the loopback redirect of the last opened page.
func (r *rig) redirectHost(t *testing.T) string {
	t.Helper()
	opened := r.browser.Opened()
	if len(opened) == 0 {
		t.Fatal("no page was opened")
	}
	u, err := url.Parse(testenv.AuthParam(opened[len(opened)-1], "redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// assertClosed checks that nothing listens on the loopback redirect any more.
func assertClosed(t *testing.T, host string) {
	t.Helper()
	if conn, err := net.DialTimeout("tcp", host, 3*time.Second); err == nil {
		_ = conn.Close()
		t.Fatalf("the loopback listener %s is still open", host)
	}
}

// assertNoSecretText checks that an error text names no secret, token, code or the authorization URL.
func assertNoSecretText(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, s := range []string{testenv.FakeGoogleClientSecret, "fake-refresh-", "fake-access-", "fake-code-", "code_challenge", "state="} {
		if strings.Contains(err.Error(), s) {
			t.Fatalf("the error %q contains %q", err, s)
		}
	}
}

// Test step 2 of TASK-050: the code exchange uses the code_verifier of the challenge (the fake checks
// BASE64URL(SHA256(verifier)) == challenge with S256), the refresh token goes to the credential store and the access
// token only to memory; the listener is bound to 127.0.0.1 and closed afterwards.
func TestSignInStoresTheRefreshTokenOnly(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	r := newRig(t)
	ctx := context.Background()

	if err := r.a.SignIn(ctx); err != nil {
		t.Fatal(err)
	}

	auth := r.google.AuthRequests()
	if len(auth) != 1 {
		t.Fatalf("authorization requests: %d", len(auth))
	}
	q := auth[0]
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Path != "" {
		t.Fatalf("redirect_uri %q is not the loopback address", q.Get("redirect_uri"))
	}
	for key, want := range map[string]string{
		"client_id": testenv.FakeGoogleClientID, "scope": values.GoogleCalendarScope, "response_type": "code",
		"code_challenge_method": "S256", "access_type": "offline", "prompt": "consent",
	} {
		if q.Get(key) != want {
			t.Errorf("authorization %s = %q, want %q", key, q.Get(key), want)
		}
	}
	if len(q.Get("state")) != 43 || q.Has("client_secret") {
		t.Errorf("state of %d characters, client secret in the URL: %v", len(q.Get("state")), q.Has("client_secret"))
	}
	if strings.Contains(r.browser.Opened()[0], testenv.FakeGoogleClientSecret) {
		t.Fatal("the authorization page carries the client secret")
	}

	tokens := r.google.TokenRequests()
	if len(tokens) != 1 {
		t.Fatalf("token requests: %d", len(tokens))
	}
	form := tokens[0]
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	if form.Get("grant_type") != "authorization_code" || form.Get("code_verifier") == "" ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") || form.Get("redirect_uri") != q.Get("redirect_uri") {
		t.Fatalf("the exchange did not use the verifier of the challenge: %v", form)
	}

	live := r.google.LiveRefreshTokens()
	if len(live) != 1 || r.stored(t, RefreshTokenName) != live[0] || r.creds.Len() != 3 {
		t.Fatalf("store holds %d secrets; refresh token stored: %v", r.creds.Len(), r.stored(t, RefreshTokenName) != "")
	}
	tok, err := r.a.Token(ctx)
	if err != nil || !r.google.IsLiveAccessToken(tok.AccessToken) || tok.RefreshToken != "" {
		t.Fatalf("Token = %v (live %v)", err, err == nil && r.google.IsLiveAccessToken(tok.AccessToken))
	}
	if n := len(r.google.TokenRequests()); n != 1 {
		t.Fatalf("the cached access token was not reused: %d token requests", n)
	}
	for _, name := range []string{ClientIDName, ClientSecretName, RefreshTokenName} {
		if r.stored(t, name) == tok.AccessToken {
			t.Fatalf("the access token is stored under %s", name)
		}
	}

	visits := r.browser.Visits()
	if len(visits) != 1 {
		t.Fatalf("visits: %d", len(visits))
	}
	v := visits[0]
	if v.Status != http.StatusOK || !strings.Contains(v.Body, "Вход выполнен") || v.Header.Get("X-Content-Type-Options") != "nosniff" ||
		v.Header.Get("Cache-Control") != "no-store" || v.Header.Get("Referrer-Policy") != "no-referrer" ||
		!strings.HasPrefix(v.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("answer page %d %v %q", v.Status, v.Header, v.Body)
	}
	if strings.Contains(v.Body, "fake-code") || strings.Contains(v.Body, q.Get("state")) {
		t.Fatal("the answer page echoes the request")
	}
	assertClosed(t, redirect.Host)
}

// A redirect without the right state, on another path or method, or for another host is answered and ignored: it
// cannot end the sign-in, and the real redirect still completes it.
func TestSignInIgnoresForeignRedirects(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	r := newRig(t)
	var statuses []int
	r.browser.Before(func(ctx context.Context, authURL string) {
		redirect := testenv.AuthParam(authURL, "redirect_uri")
		state := testenv.AuthParam(authURL, "state")
		u, _ := url.Parse(redirect)
		send := func(method, target, host string) {
			req, err := http.NewRequestWithContext(ctx, method, target, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if host != "" {
				req.Host = host
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
			statuses = append(statuses, resp.StatusCode)
		}
		send(http.MethodGet, redirect+"?code=forged&state=wrong", "")
		send(http.MethodGet, redirect+"?code=forged", "")
		send(http.MethodGet, redirect+"?code=forged&state="+state+"&state="+state, "")
		send(http.MethodGet, redirect+"?state="+state, "")
		send(http.MethodGet, redirect+"?code=forged&error=x&state="+state, "")
		send(http.MethodGet, redirect+"/x?code=forged&state="+state, "")
		send(http.MethodPost, redirect+"?code=forged&state="+state, "")
		send(http.MethodGet, redirect+"?code=forged&state="+state, "evil.example:"+u.Port())
	})

	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []int{400, 400, 400, 400, 400, 404, 405, 400}
	if len(statuses) != len(want) {
		t.Fatalf("statuses %v, want %v", statuses, want)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("statuses %v, want %v", statuses, want)
		}
	}
	if forms := r.google.TokenRequests(); len(forms) != 1 || forms[0].Get("code") == "forged" {
		t.Fatalf("token requests %d: a forged code was exchanged", len(forms))
	}
}

// The handler accepts one redirect only; a second one with the same state changes nothing.
func TestCallbackIsSingleUse(t *testing.T) {
	cb := &callback{state: "s", host: "127.0.0.1:1", result: make(chan redirectResult, 1)}
	serve := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host = "127.0.0.1:1"
		rec := httptest.NewRecorder()
		cb.ServeHTTP(rec, req)
		return rec
	}
	if rec := serve("/?code=c1&state=s"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Вход выполнен") {
		t.Fatalf("first redirect: %d %q", rec.Code, rec.Body)
	}
	if rec := serve("/?code=c2&state=s"); rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "c2") {
		t.Fatalf("second redirect: %d %q", rec.Code, rec.Body)
	}
	if res := <-cb.result; res.code != "c1" {
		t.Fatalf("result %+v", res)
	}
	select {
	case res := <-cb.result:
		t.Fatalf("a second result %+v", res)
	default:
	}
}

func TestSignInFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *rig)
		want  []error
		// revoked is whether the grant was revoked because the app does not keep it.
		revoked bool
	}{
		{name: "user denied", setup: func(r *rig) { r.google.Deny(true) }, want: []error{ErrDenied}},
		{name: "no refresh token", setup: func(r *rig) { r.google.OmitRefreshToken(true) }, want: []error{ErrNoRefreshToken}, revoked: true},
		{name: "broader scope", setup: func(r *rig) {
			r.google.GrantScope(values.GoogleCalendarScope + " https://www.googleapis.com/auth/calendar")
		}, want: []error{ErrScope, values.ErrScopeTooBroad}, revoked: true},
		{name: "calendar scope missing", setup: func(r *rig) { r.google.GrantScope("") },
			want: []error{ErrScope, values.ErrCalendarScopeNotGranted}, revoked: true},
		{name: "wrong client secret", setup: func(r *rig) {
			if err := r.creds.Set(context.Background(), ClientSecretName, "GOCSPX-other-secret"); err != nil {
				panic(err)
			}
		}, want: []error{ErrInvalidClient}},
		{name: "browser cannot start", setup: func(r *rig) { r.browser.SetMode(testenv.BrowserFail) }, want: []error{ErrBrowser, testenv.ErrFakeBrowser}},
		{name: "store fails on write", setup: func(r *rig) {
			// The client is read before the page opens; the refresh token cannot be written afterwards.
			r.browser.Before(func(context.Context, string) { r.creds.Fail(shared.ErrStorage) })
		}, want: []error{ErrCredentials, shared.ErrStorage}, revoked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.NoGoroutineLeaks(t)
			r := newRig(t)
			tt.setup(r)
			err := r.a.SignIn(context.Background())
			r.creds.Fail(nil)
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Fatalf("SignIn = %v, want %v", err, w)
				}
			}
			assertNoSecretText(t, err)
			if r.stored(t, RefreshTokenName) != "" {
				t.Fatal("a refresh token was stored")
			}
			if got := len(r.google.Revoked()) > 0; got != tt.revoked {
				t.Fatalf("revoked %v, want %v", got, tt.revoked)
			}
			if len(r.google.LiveRefreshTokens()) != 0 {
				t.Fatal("a grant the app does not keep is still alive")
			}
			assertClosed(t, r.redirectHost(t))
		})
	}
}

// The user never finishes: the sign-in ends with ErrTimeout and closes the listener.
func TestSignInTimesOut(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	r := newRig(t, WithSignInTimeout(200*time.Millisecond))
	r.browser.SetMode(testenv.BrowserIgnore)

	err := r.a.SignIn(context.Background())
	if !errors.Is(err, ErrTimeout) || errors.Is(err, context.Canceled) {
		t.Fatalf("SignIn = %v, want ErrTimeout", err)
	}
	assertClosed(t, r.redirectHost(t))
	if r.stored(t, RefreshTokenName) != "" || len(r.google.TokenRequests()) != 0 {
		t.Fatal("a timed-out sign-in exchanged or stored something")
	}
}

// A cancelled ctx ends the waiting sign-in with the ctx error, not ErrTimeout.
func TestSignInCancelled(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	r := newRig(t)
	r.browser.SetMode(testenv.BrowserIgnore)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.a.SignIn(ctx) }()

	testenv.Eventually(t, "the page is opened", func() bool { return len(r.browser.Opened()) == 1 }, nil)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrTimeout) {
			t.Fatalf("SignIn = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SignIn did not return after the cancel")
	}
	assertClosed(t, r.redirectHost(t))
}

// slowTokens holds token requests until a fixed time, giving up when the request's ctx ends first.
type slowTokens struct {
	base      http.RoundTripper
	tokenPath string
	until     time.Time
}

func (s *slowTokens) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == s.tokenPath {
		timer := time.NewTimer(time.Until(s.until))
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-req.Context().Done():
			if req.Body != nil {
				_ = req.Body.Close()
			}
			return nil, req.Context().Err()
		}
	}
	return s.base.RoundTrip(req)
}

// The sign-in timeout bounds only the wait for the redirect: a code that came in time is exchanged even when the
// exchange ends after the timeout.
func TestSignInExchangesAfterTheTimeout(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	const timeout = 300 * time.Millisecond
	tokens := &slowTokens{base: &http.Transport{DisableKeepAlives: true}}
	r := newRig(t, WithSignInTimeout(timeout), WithHTTPClient(&http.Client{Transport: tokens, Timeout: 5 * time.Second}))
	u, err := url.Parse(r.google.TokenURL())
	if err != nil {
		t.Fatal(err)
	}
	tokens.tokenPath = u.Path
	tokens.until = time.Now().Add(timeout + 200*time.Millisecond) // set before SignIn starts its timer

	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatalf("SignIn = %v", err)
	}
	if time.Now().Before(tokens.until) {
		t.Fatal("the exchange was not held past the sign-in timeout")
	}
	if live := r.google.LiveRefreshTokens(); len(live) != 1 || r.stored(t, RefreshTokenName) != live[0] {
		t.Fatal("the refresh token was not stored")
	}
}

// Without a usable client nothing is opened.
func TestSignInNeedsAClient(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *rig)
		want  error
	}{
		{name: "no client id", setup: func(r *rig) { _ = r.creds.Delete(context.Background(), ClientIDName) }, want: ErrNotConfigured},
		{name: "no client secret", setup: func(r *rig) { _ = r.creds.Delete(context.Background(), ClientSecretName) }, want: ErrNotConfigured},
		{name: "edited client id", setup: func(r *rig) { _ = r.creds.Set(context.Background(), ClientIDName, "x&y=1") }, want: ErrNotConfigured},
		{name: "no credential store", setup: func(r *rig) { r.creds.Fail(errors.ErrUnsupported) }, want: errors.ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			tt.setup(r)
			err := r.a.SignIn(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("SignIn = %v, want %v", err, tt.want)
			}
			if tt.want == errors.ErrUnsupported && !errors.Is(err, ErrCredentials) {
				t.Fatalf("SignIn = %v, want ErrCredentials", err)
			}
			if len(r.browser.Opened()) != 0 {
				t.Fatal("the browser was opened without a client")
			}
		})
	}
}

// signedIn is a rig after a successful sign-in.
func signedIn(t *testing.T, opts ...Option) *rig {
	t.Helper()
	r := newRig(t, opts...)
	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

// An expired access token is refreshed with the stored refresh token, which stays as it is.
func TestTokenRefreshes(t *testing.T) {
	r := newRig(t)
	r.google.ExpiresIn(1) // x/oauth2 treats it as expired at once
	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	refresh := r.stored(t, RefreshTokenName)

	tok, err := r.a.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	forms := r.google.TokenRequests()
	last := forms[len(forms)-1]
	if len(forms) != 2 || last.Get("grant_type") != "refresh_token" || last.Get("refresh_token") != refresh ||
		last.Get("client_secret") != testenv.FakeGoogleClientSecret {
		t.Fatalf("token requests %d, last grant %q", len(forms), last.Get("grant_type"))
	}
	if !r.google.IsLiveAccessToken(tok.AccessToken) || tok.RefreshToken != "" || r.stored(t, RefreshTokenName) != refresh {
		t.Fatal("the refreshed token is wrong or the refresh token changed")
	}
}

// A rotated refresh token replaces the stored one.
func TestTokenStoresARotatedRefreshToken(t *testing.T) {
	r := newRig(t)
	r.google.ExpiresIn(1)
	r.google.RotateRefreshTokens(true)
	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := r.stored(t, RefreshTokenName)
	if _, err := r.a.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	live := r.google.LiveRefreshTokens()
	if len(live) != 1 || live[0] == old || r.stored(t, RefreshTokenName) != live[0] {
		t.Fatalf("the rotated refresh token was not stored (live %d)", len(live))
	}
	if _, err := r.a.Token(context.Background()); err != nil {
		t.Fatalf("the next refresh with the rotated token: %v", err)
	}
}

// invalid_grant: the dead refresh token is removed, so the status says "not signed in".
func TestTokenInvalidGrantRemovesTheRefreshToken(t *testing.T) {
	r := newRig(t)
	r.google.ExpiresIn(1)
	if err := r.a.SignIn(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.google.FailRefresh("invalid_grant")

	_, err := r.a.Token(context.Background())
	if !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("Token = %v, want ErrReauthRequired", err)
	}
	assertNoSecretText(t, err)
	if r.stored(t, RefreshTokenName) != "" || r.stored(t, ClientIDName) == "" {
		t.Fatal("the dead refresh token stayed, or the client was removed")
	}
	if _, err := r.a.Token(context.Background()); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("Token after the removal = %v, want ErrNotSignedIn", err)
	}
}

// Other refusals keep the refresh token: it may work again.
func TestTokenFailuresKeepTheRefreshToken(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{code: "invalid_client", want: ErrInvalidClient},
		{code: "invalid_request", want: ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			r := newRig(t)
			r.google.ExpiresIn(1)
			if err := r.a.SignIn(context.Background()); err != nil {
				t.Fatal(err)
			}
			r.google.FailRefresh(tt.code)
			_, err := r.a.Token(context.Background())
			if !errors.Is(err, tt.want) {
				t.Fatalf("Token = %v, want %v", err, tt.want)
			}
			assertNoSecretText(t, err)
			if r.stored(t, RefreshTokenName) == "" {
				t.Fatal("the refresh token was removed")
			}
		})
	}
}

func TestTokenNotSignedIn(t *testing.T) {
	r := newRig(t)
	if _, err := r.a.Token(context.Background()); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("Token = %v, want ErrNotSignedIn", err)
	}
}

// The client of the Calendar adapter sends the access token as a bearer token.
func TestHTTPClientSendsTheAccessToken(t *testing.T) {
	r := signedIn(t)
	var auth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		auth = req.Header.Get("Authorization")
	}))
	t.Cleanup(api.Close)

	resp, err := r.a.HTTPClient().Get(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || !r.google.IsLiveAccessToken(token) {
		t.Fatalf("Authorization %q is not a live bearer token", auth)
	}

	if err := r.a.SignOut(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.HTTPClient().Get(api.URL); !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("Get after sign-out = %v, want ErrNotSignedIn", err)
	}
}

// The bearer client follows redirects on the same host only: the token must not reach another host.
func TestHTTPClientRefusesForeignRedirects(t *testing.T) {
	r := signedIn(t)
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { foreignHits.Add(1) }))
	t.Cleanup(foreign.Close)
	var auth atomic.Value // the Authorization header the same-host target got
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/same":
			http.Redirect(w, req, "/target", http.StatusFound)
		case "/foreign":
			http.Redirect(w, req, foreign.URL+"/target", http.StatusFound)
		case "/loop":
			http.Redirect(w, req, "/loop", http.StatusFound)
		case "/target":
			auth.Store(req.Header.Get("Authorization"))
		}
	}))
	t.Cleanup(api.Close)

	tests := []struct {
		path string
		want error
	}{
		{path: "/same"},
		{path: "/foreign", want: errForeignRedirect},
		{path: "/loop", want: errTooManyRedirects},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			auth.Store("")
			resp, err := r.a.HTTPClient().Get(api.URL + tt.path)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("Get = %v, want %v", err, tt.want)
			}
			if got, _ := auth.Load().(string); tt.want == nil && !strings.HasPrefix(got, "Bearer ") {
				t.Fatalf("the same-host target got Authorization %q", got)
			}
		})
	}
	if n := foreignHits.Load(); n != 0 {
		t.Fatalf("the foreign host got %d requests", n)
	}
}

func TestSignOut(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) *rig
		want    error
		revoked int
	}{
		{name: "revokes and removes", setup: func(t *testing.T) *rig { return signedIn(t) }, revoked: 1},
		{name: "revocation not reachable", setup: func(t *testing.T) *rig {
			r := signedIn(t)
			r.google.DropRevoke(true)
			return r
		}, want: ErrRevokeFailed, revoked: 1},
		{name: "token already revoked", setup: func(t *testing.T) *rig {
			r := newRig(t)
			if err := r.creds.Set(context.Background(), RefreshTokenName, "fake-refresh-dead"); err != nil {
				t.Fatal(err)
			}
			return r
		}, revoked: 1},
		{name: "not signed in", setup: func(t *testing.T) *rig { return newRig(t) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.setup(t)
			refresh := r.stored(t, RefreshTokenName)
			err := r.a.SignOut(context.Background())
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("SignOut = %v, want %v", err, tt.want)
			}
			if tt.want != nil && !errors.Is(err, ErrNetwork) {
				t.Fatalf("SignOut = %v, want a network cause", err)
			}
			assertNoSecretText(t, err)
			revoked := r.google.Revoked()
			if len(revoked) != tt.revoked || (tt.revoked > 0 && revoked[0] != refresh) {
				t.Fatalf("revoked %d tokens, want %d (the refresh token)", len(revoked), tt.revoked)
			}
			if r.stored(t, RefreshTokenName) != "" || r.stored(t, ClientIDName) == "" || r.stored(t, ClientSecretName) == "" {
				t.Fatal("sign-out must remove the refresh token and keep the client")
			}
			if _, err := r.a.Token(context.Background()); !errors.Is(err, ErrNotSignedIn) {
				t.Fatalf("Token after sign-out = %v: the cached access token survived", err)
			}
		})
	}
}

func TestNewValidates(t *testing.T) {
	creds, b := testenv.NewMemCredentials(), testenv.NewFakeBrowser()
	for name, build := range map[string]func() (*Authorizer, error){
		"no store":        func() (*Authorizer, error) { return New(nil, b) },
		"no browser":      func() (*Authorizer, error) { return New(creds, nil) },
		"no http client":  func() (*Authorizer, error) { return New(creds, b, WithHTTPClient(nil)) },
		"zero timeout":    func() (*Authorizer, error) { return New(creds, b, WithSignInTimeout(0)) },
		"empty endpoints": func() (*Authorizer, error) { return New(creds, b, WithEndpoints("", "", "")) },
	} {
		if _, err := build(); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: New = %v, want ErrInvalidConfig", name, err)
		}
	}
	a, err := New(creds, b)
	if err != nil || a.authURL != AuthURL || a.tokenURL != TokenURL || a.revokeURL != RevokeURL ||
		a.http.Timeout != DefaultHTTPTimeout || a.signInTimeout != DefaultSignInTimeout {
		t.Fatalf("defaults: %v", err)
	}
}
