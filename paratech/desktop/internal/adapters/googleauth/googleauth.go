// Package googleauth authorizes the app to the Google Calendar API (PRD sections 4.3, 11.3 and 12; TASK-050):
// OAuth 2.0 for installed apps with the Authorization Code flow, PKCE (S256), the system browser and a loopback
// redirect to 127.0.0.1, through golang.org/x/oauth2.
//
// The OAuth client is the user's own Google Cloud "Desktop" client: its id and secret are entered on the
// «Подключения» page and kept in the credential store, never in the binary (Google documents the secret of an
// installed app as not confidential; the app always sends the secret the user entered). The only scope is
// values.GoogleCalendarScope; a token response that grants anything else is revoked.
//
// The refresh token lives only in the credential store (AC-19, hard rule 4); the access token only in memory. Codes,
// verifiers, states, tokens, the client secret and the authorization URL are never logged and never put in an error.
//
// Rules that apply: ARCH-30..34. The package imports only std, x/oauth2 and the domain.
package googleauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"paratech/desktop/internal/domain/schedule/values"
	"paratech/desktop/internal/domain/shared"
)

// Google endpoints of an installed (Desktop) client; see docs/refs/google-calendar.md.
const (
	AuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL  = "https://oauth2.googleapis.com/token" //nolint:gosec // G101: a public endpoint, not a credential
	RevokeURL = "https://oauth2.googleapis.com/revoke"
)

// Entries of the google credential store ("paratech/google/<name>" in Credential Manager).
const (
	ClientIDName     = "client-id"
	ClientSecretName = "client-secret"
	RefreshTokenName = "refresh-token"
)

// Defaults of the options.
const (
	// DefaultSignInTimeout bounds the wait for the redirect: the user has this long to finish in the browser.
	DefaultSignInTimeout = 5 * time.Minute
	// DefaultHTTPTimeout bounds one call to the token or revocation endpoint.
	DefaultHTTPTimeout = 30 * time.Second
)

// Operation names, for error texts.
const (
	opSignIn  = "googleauth: sign in"
	opAccess  = "googleauth: access token"
	opSignOut = "googleauth: sign out"
	opClient  = "googleauth: client"
)

// Credentials is the google credential store (wincred.Store in the app, testenv.MemCredentials in tests).
type Credentials interface {
	// Get returns the secret under name, or an error wrapping shared.ErrNotFound.
	Get(ctx context.Context, name string) (string, error)
	// Set stores or replaces the secret under name.
	Set(ctx context.Context, name, secret string) error
	// Delete removes the secret under name; a missing secret is not an error.
	Delete(ctx context.Context, name string) error
}

// Browser opens the authorization page in the system browser (adapters/weblink in the app). It returns once the
// browser was asked to open the page, not when the user is done.
type Browser interface {
	OpenAuthPage(ctx context.Context, authURL string) error
}

// Option changes a default of New.
type Option func(*Authorizer)

// WithEndpoints replaces the Google endpoints (tests: a fake OAuth server).
func WithEndpoints(authURL, tokenURL, revokeURL string) Option {
	return func(a *Authorizer) { a.authURL, a.tokenURL, a.revokeURL = authURL, tokenURL, revokeURL }
}

// WithHTTPClient replaces the client of the token and revocation calls; its Timeout bounds each call.
func WithHTTPClient(c *http.Client) Option {
	return func(a *Authorizer) { a.http = c }
}

// WithSignInTimeout replaces DefaultSignInTimeout.
func WithSignInTimeout(d time.Duration) Option {
	return func(a *Authorizer) { a.signInTimeout = d }
}

// Authorizer signs in, keeps the access token in memory, refreshes it from the stored refresh token and signs out.
// It is safe for concurrent use.
type Authorizer struct {
	creds   Credentials
	browser Browser

	authURL, tokenURL, revokeURL string
	http                         *http.Client
	signInTimeout                time.Duration

	// mu serializes every change of the stored refresh token and of cached, so a refresh cannot store a rotated token
	// after a sign-out removed the old one.
	mu     sync.Mutex
	cached *oauth2.Token // the access token only, never the refresh token
}

// New returns an Authorizer over the google credential store and the browser.
func New(creds Credentials, browser Browser, opts ...Option) (*Authorizer, error) {
	if creds == nil || browser == nil {
		return nil, fmt.Errorf("%w: the credential store and the browser are required", ErrInvalidConfig)
	}
	a := &Authorizer{
		creds: creds, browser: browser, authURL: AuthURL, tokenURL: TokenURL, revokeURL: RevokeURL,
		http: &http.Client{Timeout: DefaultHTTPTimeout}, signInTimeout: DefaultSignInTimeout,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.http == nil || a.signInTimeout <= 0 || a.authURL == "" || a.tokenURL == "" || a.revokeURL == "" {
		return nil, fmt.Errorf("%w: endpoints, an HTTP client and a positive timeout are required", ErrInvalidConfig)
	}
	return a, nil
}

// client is the stored OAuth client.
type client struct{ id, secret string }

// readClient reads the client id and secret and checks them again: the store may have been edited outside the app.
func (a *Authorizer) readClient(ctx context.Context) (client, error) {
	id, err := a.creds.Get(ctx, ClientIDName)
	if err != nil {
		return client{}, storeError(opClient, err)
	}
	secret, err := a.creds.Get(ctx, ClientSecretName)
	if err != nil {
		return client{}, storeError(opClient, err)
	}
	if _, err := values.ParseGoogleClientID(id); err != nil {
		return client{}, fmt.Errorf("%s: %w: the stored client id is invalid", opClient, ErrNotConfigured)
	}
	if _, err := values.ParseGoogleClientSecret(secret); err != nil {
		return client{}, fmt.Errorf("%s: %w: the stored client secret is invalid", opClient, ErrNotConfigured)
	}
	return client{id: id, secret: secret}, nil
}

// config is the x/oauth2 configuration of c; redirect is "" outside a sign-in.
func (a *Authorizer) config(c client, redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: c.id, ClientSecret: c.secret, RedirectURL: redirect, Scopes: []string{values.GoogleCalendarScope},
		// Desktop clients send the id and secret in the form; a fixed style avoids x/oauth2's probing request.
		Endpoint: oauth2.Endpoint{AuthURL: a.authURL, TokenURL: a.tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// httpContext makes x/oauth2 use the bounded client of the adapter.
func (a *Authorizer) httpContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, a.http)
}

// storeError classifies a failure of the credential store: a missing entry is "not configured" (client) or "not
// signed in" (refresh token, mapped by the caller). The store's errors carry no secret.
func storeError(op string, err error) error {
	if errors.Is(err, shared.ErrNotFound) {
		return fmt.Errorf("%s: %w", op, ErrNotConfigured)
	}
	return fmt.Errorf("%s: %w: %w", op, ErrCredentials, err)
}
