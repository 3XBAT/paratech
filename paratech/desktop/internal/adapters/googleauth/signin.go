package googleauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"

	"paratech/desktop/internal/domain/schedule/values"
)

// Limits of the loopback redirect server.
const (
	stateBytes        = 32
	readHeaderTimeout = 10 * time.Second
	ioTimeout         = 30 * time.Second
	maxHeaderBytes    = 8 << 10
	shutdownTimeout   = 5 * time.Second
)

// oauthDenied is the error of the redirect when the user refuses the consent screen.
const oauthDenied = "access_denied"

// Fixed answer pages of the redirect (Russian UI). They never echo anything of the request.
const (
	pageDone = `<!doctype html><html lang="ru"><head><meta charset="utf-8"><title>Learning Assistant</title></head>` +
		`<body><p>Вход выполнен, вернитесь в приложение.</p></body></html>`
	pageFailed = `<!doctype html><html lang="ru"><head><meta charset="utf-8"><title>Learning Assistant</title></head>` +
		`<body><p>Вход не выполнен. Вернитесь в приложение.</p></body></html>`
)

// SignIn runs the browser sign-in and stores the refresh token: it opens a loopback listener on 127.0.0.1, asks the
// browser to open Google's consent page with a PKCE challenge and a random state, waits for the one redirect that
// carries that state, exchanges the code with the verifier and keeps the tokens if Google granted exactly
// calendar.app.created. Redirects with another state are answered 400 and ignored, so another local process cannot
// end the sign-in. It ends with ErrTimeout after the sign-in timeout and with the ctx error when ctx is done; the
// listener is closed before it returns. A new sign-in replaces the stored refresh token.
func (a *Authorizer) SignIn(ctx context.Context) error {
	c, err := a.readClient(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeoutCause(ctx, a.signInTimeout, ErrTimeout)
	defer cancel()

	cfg, code, verifier, err := a.authorize(ctx, c)
	if err != nil {
		return err
	}
	tok, err := cfg.Exchange(a.httpContext(ctx), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return tokenError(ctx, opSignIn, err)
	}
	return a.keep(ctx, tok)
}

// authorize runs the browser part and returns the configuration with the redirect URI, the code and the verifier.
func (a *Authorizer) authorize(ctx context.Context, c client) (*oauth2.Config, string, string, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0") // never 0.0.0.0 or "localhost"
	if err != nil {
		return nil, "", "", fmt.Errorf("%s: %w: %w", opSignIn, ErrLoopback, err)
	}
	host := ln.Addr().String()
	state, err := newState()
	if err != nil {
		_ = ln.Close()
		return nil, "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	// No path, as in Google's loopback examples; the browser then requests "/".
	cfg := a.config(c, "http://"+host)

	cb := &callback{state: state, host: host, result: make(chan redirectResult, 1)}
	srv := &http.Server{
		Handler: cb, ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: ioTimeout, WriteTimeout: ioTimeout,
		IdleTimeout: ioTimeout, MaxHeaderBytes: maxHeaderBytes, ErrorLog: log.New(io.Discard, "", 0),
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	defer stopServer(srv, served)

	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	if err := a.browser.OpenAuthPage(ctx, authURL); err != nil {
		if ctx.Err() != nil {
			return nil, "", "", fmt.Errorf("%s: %w", opSignIn, doneError(ctx))
		}
		// The browser adapters have fixed error texts without the URL.
		return nil, "", "", fmt.Errorf("%s: %w: %w", opSignIn, ErrBrowser, err)
	}

	select {
	case res := <-cb.result:
		switch {
		case res.err == oauthDenied:
			return nil, "", "", fmt.Errorf("%s: %w", opSignIn, ErrDenied)
		case res.err != "":
			return nil, "", "", fmt.Errorf("%s: %w", opSignIn, ErrAuthFailed)
		}
		return cfg, res.code, verifier, nil
	case <-ctx.Done():
		return nil, "", "", fmt.Errorf("%s: %w", opSignIn, doneError(ctx))
	}
}

// keep stores the refresh token and caches the access token after checking the grant; a grant the app must not
// hold is revoked.
func (a *Authorizer) keep(ctx context.Context, tok *oauth2.Token) error {
	scope, _ := tok.Extra("scope").(string)
	if err := values.CheckGrantedScopes(scope); err != nil {
		a.revokeQuietly(ctx, tok)
		return fmt.Errorf("%s: %w: %w", opSignIn, ErrScope, err)
	}
	if tok.RefreshToken == "" {
		a.revokeQuietly(ctx, tok)
		return fmt.Errorf("%s: %w", opSignIn, ErrNoRefreshToken)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// The grant exists at Google now: a cancel this late must not lose it.
	if err := a.creds.Set(context.WithoutCancel(ctx), RefreshTokenName, tok.RefreshToken); err != nil {
		a.revokeQuietly(ctx, tok)
		return fmt.Errorf("%s: %w: %w", opSignIn, ErrCredentials, err)
	}
	a.cached = accessOnly(tok)
	return nil
}

// revokeQuietly ends a grant that will not be kept, best effort: the refresh token ends the whole grant.
func (a *Authorizer) revokeQuietly(ctx context.Context, tok *oauth2.Token) {
	token := tok.RefreshToken
	if token == "" {
		token = tok.AccessToken
	}
	if token != "" {
		_ = a.revoke(context.WithoutCancel(ctx), token)
	}
}

// newState is 32 random bytes, base64url.
func newState() (string, error) {
	b := make([]byte, stateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("%s: random state: %w", opSignIn, err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// stopServer closes the listener and the connections and waits for Serve to return.
func stopServer(srv *http.Server, served <-chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	<-served
}

// redirectResult is what the one accepted redirect carried: a code or an OAuth error code.
type redirectResult struct {
	code, err string
}

// callback serves the loopback redirect. Only GET / on the listener's own host with exactly the expected state and
// either one code or one error is accepted, once; everything else gets a fixed page and changes nothing.
type callback struct {
	state  string
	host   string
	done   atomic.Bool
	result chan redirectResult // buffered: the one send never blocks
}

func (c *callback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path != "/":
		writePage(w, http.StatusNotFound, pageFailed)
		return
	case r.Method != http.MethodGet:
		w.Header().Set("Allow", http.MethodGet)
		writePage(w, http.StatusMethodNotAllowed, pageFailed)
		return
	case r.Host != c.host:
		writePage(w, http.StatusBadRequest, pageFailed)
		return
	}

	q := r.URL.Query()
	states, codes, errs := q["state"], q["code"], q["error"]
	if len(states) != 1 || subtle.ConstantTimeCompare([]byte(states[0]), []byte(c.state)) != 1 {
		writePage(w, http.StatusBadRequest, pageFailed)
		return
	}
	var res redirectResult
	switch {
	case len(codes) == 1 && codes[0] != "" && len(errs) == 0:
		res.code = codes[0]
	case len(errs) == 1 && errs[0] != "" && len(codes) == 0:
		res.err = errs[0]
	default:
		writePage(w, http.StatusBadRequest, pageFailed)
		return
	}
	if !c.done.CompareAndSwap(false, true) {
		writePage(w, http.StatusBadRequest, pageFailed)
		return
	}
	c.result <- res

	if res.err != "" {
		writePage(w, http.StatusOK, pageFailed)
		return
	}
	writePage(w, http.StatusOK, pageDone)
}

// writePage answers with a fixed page that the browser neither caches, sniffs, frames nor refers from.
func writePage(w http.ResponseWriter, status int, page string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, page)
}
