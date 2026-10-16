package googleauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"paratech/desktop/internal/domain/shared"
)

// maxRevokeBody bounds what is read of a revocation answer.
const maxRevokeBody = 64 << 10

// Token returns a valid access token for the Calendar API (TASK-051). It reuses the cached one and otherwise
// refreshes it with the stored refresh token; a rotated refresh token from Google replaces the stored one. When
// Google rejects the refresh token (invalid_grant) it is removed and the error is ErrReauthRequired. The returned
// token carries no refresh token.
func (a *Authorizer) Token(ctx context.Context) (*oauth2.Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached.Valid() {
		return accessOnly(a.cached), nil
	}

	refresh, err := a.creds.Get(ctx, RefreshTokenName)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, fmt.Errorf("%s: %w", opAccess, ErrNotSignedIn)
	case err != nil:
		return nil, fmt.Errorf("%s: %w: %w", opAccess, ErrCredentials, err)
	}
	c, err := a.readClient(ctx)
	if err != nil {
		return nil, err
	}

	tok, err := a.config(c, "").TokenSource(a.httpContext(ctx), &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		if code, ok := oauthCode(err); ok && code == "invalid_grant" && ctx.Err() == nil {
			a.cached = nil
			if derr := a.creds.Delete(context.WithoutCancel(ctx), RefreshTokenName); derr != nil {
				return nil, fmt.Errorf("%s: %w; %w: %w", opAccess, ErrReauthRequired, ErrCredentials, derr)
			}
			return nil, fmt.Errorf("%s: %w", opAccess, ErrReauthRequired)
		}
		return nil, tokenError(ctx, opAccess, err)
	}

	a.cached = accessOnly(tok)
	if tok.RefreshToken != "" && tok.RefreshToken != refresh {
		if err := a.creds.Set(context.WithoutCancel(ctx), RefreshTokenName, tok.RefreshToken); err != nil {
			// The old token may be retired already; the next refresh will say ErrReauthRequired.
			return nil, fmt.Errorf("%s: store the rotated refresh token: %w: %w", opAccess, ErrCredentials, err)
		}
	}
	return accessOnly(tok), nil
}

// HTTPClient returns a client that sends every request with "Authorization: Bearer <access token>" from Token,
// refreshed as needed, through the transport of the adapter's HTTP client.
func (a *Authorizer) HTTPClient() *http.Client {
	base := a.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{Transport: &bearer{a: a, base: base}, Timeout: a.http.Timeout}
}

// bearer is the RoundTripper of HTTPClient. The refresh uses the request's context.
type bearer struct {
	a    *Authorizer
	base http.RoundTripper
}

func (b *bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := b.a.Token(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close() // RoundTrip must close the body, also on errors
		}
		return nil, err
	}
	r := req.Clone(req.Context())
	tok.SetAuthHeader(r)
	return b.base.RoundTrip(r)
}

// SignOut asks Google to revoke the grant and always removes the stored refresh token and the cached access token,
// even when the revocation fails (then the error is ErrRevokeFailed: the user can revoke the access at
// https://myaccount.google.com/permissions). Without a stored token it only clears. The client id and secret stay.
func (a *Authorizer) SignOut(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cached = nil

	var revokeErr error
	refresh, err := a.creds.Get(ctx, RefreshTokenName)
	switch {
	case err == nil:
		if err := a.revoke(ctx, refresh); err != nil {
			revokeErr = fmt.Errorf("%s: %w", opSignOut, err)
		}
	case !errors.Is(err, shared.ErrNotFound):
		revokeErr = fmt.Errorf("%s: %w: %w", opSignOut, ErrCredentials, err)
	}
	if err := a.creds.Delete(context.WithoutCancel(ctx), RefreshTokenName); err != nil {
		return errors.Join(fmt.Errorf("%s: %w: %w", opSignOut, ErrCredentials, err), revokeErr)
	}
	return revokeErr
}

// revoke posts the token to the revocation endpoint. 200 is done; 400 invalid_token means the token is already
// revoked or expired, which is done too. Anything else is ErrRevokeFailed.
func (a *Authorizer) revoke(ctx context.Context, token string) error {
	form := url.Values{"token": {token}}.Encode() // in the body, never in the URL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.revokeURL, strings.NewReader(form))
	if err != nil {
		return fmt.Errorf("%w: bad endpoint", ErrRevokeFailed)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrRevokeFailed, ctx.Err())
		}
		return fmt.Errorf("%w: %w", ErrRevokeFailed, ErrNetwork)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRevokeBody))
	switch {
	case err != nil:
		return fmt.Errorf("%w: %w", ErrRevokeFailed, ErrNetwork)
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusBadRequest:
		var answer struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &answer) == nil && answer.Error == "invalid_token" {
			return nil
		}
	}
	return fmt.Errorf("%w: status %d", ErrRevokeFailed, resp.StatusCode)
}

// accessOnly copies the access part of tok: the refresh token never sits in memory longer than a call.
func accessOnly(tok *oauth2.Token) *oauth2.Token {
	return &oauth2.Token{AccessToken: tok.AccessToken, TokenType: tok.TokenType, Expiry: tok.Expiry}
}
