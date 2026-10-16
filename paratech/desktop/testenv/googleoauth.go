package testenv

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Obviously fake values of the Google OAuth client a FakeGoogleOAuth accepts.
const (
	FakeGoogleClientID     = "123-fake.apps.googleusercontent.com"
	FakeGoogleClientSecret = "GOCSPX-fake-client-secret"
)

// Paths of the fake endpoints.
const (
	fakeGoogleAuthPath   = "/o/oauth2/v2/auth"
	fakeGoogleTokenPath  = "/token"
	fakeGoogleRevokePath = "/revoke"
)

// FakeGoogleOAuth is a local Google OAuth server for the sign-in tests (TASK-050): the authorization endpoint of the
// browser, the token endpoint (authorization_code and refresh_token grants) and the revocation endpoint. It checks
// what Google checks for a Desktop client with PKCE: the client id and secret, a loopback redirect, the single use of
// a code, the redirect URI of the exchange and BASE64URL(SHA256(code_verifier)) == code_challenge with method S256.
// It records every request, so tests can look for the secrets in it, and it serves plain HTTP on 127.0.0.1.
type FakeGoogleOAuth struct {
	srv *httptest.Server

	mu         sync.Mutex
	deny       bool   // the user refuses consent: redirect with error=access_denied
	scope      string // the scope of the token responses
	noRefresh  bool   // the exchange answers without a refresh token
	rotate     bool   // a refresh answers with a new refresh token and retires the old one
	refreshErr string
	revokeDrop bool
	expiresIn  int

	seq     int
	codes   map[string]fakeGoogleGrant
	refresh map[string]bool // live refresh tokens
	access  map[string]bool // live access tokens

	authRequests  []url.Values
	tokenRequests []url.Values
	revoked       []string
}

// fakeGoogleGrant is an issued authorization code.
type fakeGoogleGrant struct {
	challenge, redirect string
}

// StartFakeGoogleOAuth serves a fake Google OAuth server until the test ends. It grants the calendar.app.created
// scope with an access token valid for an hour.
func StartFakeGoogleOAuth(t testing.TB) *FakeGoogleOAuth {
	t.Helper()
	f := &FakeGoogleOAuth{
		scope: "https://www.googleapis.com/auth/calendar.app.created", expiresIn: 3599,
		codes: map[string]fakeGoogleGrant{}, refresh: map[string]bool{}, access: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(fakeGoogleAuthPath, f.authorize)
	mux.HandleFunc(fakeGoogleTokenPath, f.token)
	mux.HandleFunc(fakeGoogleRevokePath, f.revoke)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// AuthURL, TokenURL and RevokeURL are the endpoints.
func (f *FakeGoogleOAuth) AuthURL() string   { return f.srv.URL + fakeGoogleAuthPath }
func (f *FakeGoogleOAuth) TokenURL() string  { return f.srv.URL + fakeGoogleTokenPath }
func (f *FakeGoogleOAuth) RevokeURL() string { return f.srv.URL + fakeGoogleRevokePath }

// Deny makes the user refuse the consent screen; false grants again.
func (f *FakeGoogleOAuth) Deny(deny bool) { f.set(func() { f.deny = deny }) }

// GrantScope sets the scope list of the token responses (for example a broader scope than requested).
func (f *FakeGoogleOAuth) GrantScope(scope string) { f.set(func() { f.scope = scope }) }

// OmitRefreshToken makes the code exchange answer without a refresh token.
func (f *FakeGoogleOAuth) OmitRefreshToken(omit bool) { f.set(func() { f.noRefresh = omit }) }

// RotateRefreshTokens makes every refresh answer with a new refresh token and retire the old one.
func (f *FakeGoogleOAuth) RotateRefreshTokens(rotate bool) { f.set(func() { f.rotate = rotate }) }

// FailRefresh makes the refresh grant answer 400 with the OAuth error code (for example "invalid_grant"); "" heals.
func (f *FakeGoogleOAuth) FailRefresh(code string) { f.set(func() { f.refreshErr = code }) }

// DropRevoke makes the revocation endpoint close the connection without an answer (a network failure).
func (f *FakeGoogleOAuth) DropRevoke(drop bool) { f.set(func() { f.revokeDrop = drop }) }

// ExpiresIn sets the lifetime in seconds of the access tokens issued from now on; 1 makes x/oauth2 treat each one as
// expired at once (it subtracts 10 seconds), so every use refreshes.
func (f *FakeGoogleOAuth) ExpiresIn(seconds int) { f.set(func() { f.expiresIn = seconds }) }

// AuthRequests returns the query of every authorization request, in order.
func (f *FakeGoogleOAuth) AuthRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneValues(f.authRequests)
}

// TokenRequests returns the form of every token request, in order.
func (f *FakeGoogleOAuth) TokenRequests() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return cloneValues(f.tokenRequests)
}

// Revoked returns the tokens of every revocation request, in order.
func (f *FakeGoogleOAuth) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}

// LiveRefreshTokens returns the refresh tokens that still work.
func (f *FakeGoogleOAuth) LiveRefreshTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for t, ok := range f.refresh {
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// IsLiveAccessToken reports whether the access token was issued and not revoked.
func (f *FakeGoogleOAuth) IsLiveAccessToken(token string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.access[token]
}

func (f *FakeGoogleOAuth) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// authorize is the consent screen: it redirects the browser to the loopback redirect URI with a code or an error.
func (f *FakeGoogleOAuth) authorize(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.authRequests = append(f.authRequests, cloneQuery(q))

	redirect, err := url.Parse(q.Get("redirect_uri"))
	switch {
	case r.Method != http.MethodGet:
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	case q.Get("client_id") != FakeGoogleClientID:
		http.Error(w, "invalid_client", http.StatusUnauthorized) // Google shows an error page, no redirect
		return
	case err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" ||
		redirect.Path != "" || redirect.RawQuery != "" || redirect.User != nil || redirect.Fragment != "":
		http.Error(w, "redirect_uri_mismatch", http.StatusBadRequest)
		return
	case q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		q.Get("state") == "" || q.Get("scope") == "":
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}

	back := url.Values{"state": {q.Get("state")}}
	if f.deny {
		back.Set("error", "access_denied")
	} else {
		f.seq++
		code := fmt.Sprintf("fake-code-%d", f.seq)
		f.codes[code] = fakeGoogleGrant{challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		back.Set("code", code)
		back.Set("scope", q.Get("scope"))
	}
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// token is the token endpoint of a Desktop client: the client id and secret in the form (AuthStyleInParams).
func (f *FakeGoogleOAuth) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	form := cloneQuery(r.PostForm)
	f.tokenRequests = append(f.tokenRequests, form)

	if form.Get("client_id") != FakeGoogleClientID || form.Get("client_secret") != FakeGoogleClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		f.exchange(w, form)
	case "refresh_token":
		f.refreshGrant(w, form)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (f *FakeGoogleOAuth) exchange(w http.ResponseWriter, form url.Values) {
	code := form.Get("code")
	grant, ok := f.codes[code]
	delete(f.codes, code) // single use, also after a failed attempt
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !ok || form.Get("redirect_uri") != grant.redirect || form.Get("code_verifier") == "" ||
		subtle.ConstantTimeCompare([]byte(challenge), []byte(grant.challenge)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	refresh := ""
	if !f.noRefresh {
		refresh = f.newRefreshToken()
	}
	f.issue(w, refresh)
}

func (f *FakeGoogleOAuth) refreshGrant(w http.ResponseWriter, form url.Values) {
	if f.refreshErr != "" {
		oauthError(w, http.StatusBadRequest, f.refreshErr)
		return
	}
	old := form.Get("refresh_token")
	if !f.refresh[old] {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	refresh := ""
	if f.rotate {
		f.refresh[old] = false
		refresh = f.newRefreshToken()
	}
	f.issue(w, refresh)
}

func (f *FakeGoogleOAuth) newRefreshToken() string {
	f.seq++
	t := fmt.Sprintf("fake-refresh-%d", f.seq)
	f.refresh[t] = true
	return t
}

// issue answers a token request with a new access token and refresh, if not "".
func (f *FakeGoogleOAuth) issue(w http.ResponseWriter, refresh string) {
	f.seq++
	access := fmt.Sprintf("fake-access-%d", f.seq)
	f.access[access] = true
	body := map[string]any{
		"access_token": access, "expires_in": f.expiresIn, "scope": f.scope, "token_type": "Bearer",
	}
	if refresh != "" {
		body["refresh_token"] = refresh
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}

// revoke is the revocation endpoint: 200 for a live token, 400 invalid_token for an unknown or revoked one.
func (f *FakeGoogleOAuth) revoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.mu.Lock()
	token := r.PostForm.Get("token")
	f.revoked = append(f.revoked, token)
	drop := f.revokeDrop
	live := f.refresh[token] || f.access[token]
	if live && !drop {
		// Revoking any token of the grant ends the whole grant (the fake has one user and one client).
		f.refresh, f.access = map[string]bool{}, map[string]bool{}
	}
	f.mu.Unlock()

	if drop {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		panic(http.ErrAbortHandler)
	}
	if !live {
		oauthError(w, http.StatusBadRequest, "invalid_token")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "fake google: " + code})
}

func cloneQuery(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func cloneValues(in []url.Values) []url.Values {
	out := make([]url.Values, len(in))
	for i, v := range in {
		out[i] = cloneQuery(v)
	}
	return out
}

// BrowserMode is what a FakeBrowser does with an authorization page.
type BrowserMode int

const (
	// BrowserFollow opens the page and follows its redirects, as a user who signs in at once.
	BrowserFollow BrowserMode = iota
	// BrowserIgnore records the page and does nothing, as a user who never finishes.
	BrowserIgnore
	// BrowserFail fails to start, as a missing browser.
	BrowserFail
)

// ErrFakeBrowser is the start failure of BrowserFail.
var ErrFakeBrowser = errors.New("fake browser: cannot start")

// BrowserVisit is the last page a FakeBrowser showed: the answer of the loopback redirect.
type BrowserVisit struct {
	Status int
	Header http.Header
	Body   string
}

// FakeBrowser is the system browser of the sign-in tests: it implements OpenAuthPage like adapters/weblink, but GETs
// the page itself with a client that follows redirects and keeps no connections open.
type FakeBrowser struct {
	client *http.Client

	mu     sync.Mutex
	mode   BrowserMode
	before func(ctx context.Context, authURL string)
	opened []string
	visits []BrowserVisit
}

// NewFakeBrowser returns a browser in BrowserFollow mode.
func NewFakeBrowser() *FakeBrowser {
	return &FakeBrowser{client: &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 10 * time.Second,
	}}
}

// SetMode changes what later OpenAuthPage calls do.
func (b *FakeBrowser) SetMode(m BrowserMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mode = m
}

// Before runs fn with the page before it is followed (for example to send a forged redirect first).
func (b *FakeBrowser) Before(fn func(ctx context.Context, authURL string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.before = fn
}

// OpenAuthPage opens the authorization page as BrowserMode says.
func (b *FakeBrowser) OpenAuthPage(ctx context.Context, authURL string) error {
	b.mu.Lock()
	b.opened = append(b.opened, authURL)
	mode, before := b.mode, b.before
	b.mu.Unlock()

	switch mode {
	case BrowserFail:
		return ErrFakeBrowser
	case BrowserIgnore:
		return nil
	}
	if before != nil {
		before(ctx, authURL)
	}
	visit, err := b.Get(ctx, authURL)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.visits = append(b.visits, visit)
	b.mu.Unlock()
	return nil
}

// Get loads a page with the browser's client, following redirects.
func (b *FakeBrowser) Get(ctx context.Context, page string) (BrowserVisit, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return BrowserVisit{}, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return BrowserVisit{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return BrowserVisit{}, err
	}
	return BrowserVisit{Status: resp.StatusCode, Header: resp.Header.Clone(), Body: string(body)}, nil
}

// Opened returns the pages OpenAuthPage got, in order.
func (b *FakeBrowser) Opened() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.opened...)
}

// Visits returns the last page of every followed authorization, in order.
func (b *FakeBrowser) Visits() []BrowserVisit {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]BrowserVisit(nil), b.visits...)
}

// AuthParam returns a query parameter of an authorization page (for example "redirect_uri" or "state"), or "".
func AuthParam(authURL, key string) string {
	u, err := url.Parse(authURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}
