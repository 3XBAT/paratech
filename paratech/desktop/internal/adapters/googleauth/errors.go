package googleauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	"golang.org/x/oauth2"

	"paratech/desktop/internal/domain/shared"
)

// Errors of the adapter (ARCH-32). Their texts are fixed: no Google answer, token, code or secret is ever in them.
var (
	// ErrInvalidConfig is returned by New for a missing dependency or an unusable option.
	ErrInvalidConfig = errors.New("googleauth: invalid configuration")

	// ErrNotConfigured means the client id or secret is not stored (or is invalid).
	ErrNotConfigured = errors.New("googleauth: the google client is not configured")

	// ErrNotSignedIn means no refresh token is stored.
	ErrNotSignedIn = errors.New("googleauth: not signed in to google")

	// ErrCredentials means the credential store failed; it wraps the store error (errors.ErrUnsupported off Windows).
	ErrCredentials = errors.New("googleauth: credential store unavailable")

	// ErrLoopback means the loopback redirect listener could not be opened.
	ErrLoopback = errors.New("googleauth: cannot listen on 127.0.0.1")

	// ErrBrowser means the system browser could not be asked to open the page.
	ErrBrowser = errors.New("googleauth: cannot open the browser")

	// ErrTimeout means the user did not finish in the browser within the sign-in timeout.
	ErrTimeout = errors.New("googleauth: the sign-in timed out")

	// ErrDenied means the user refused the consent screen (error=access_denied).
	ErrDenied = errors.New("googleauth: the user denied access")

	// ErrAuthFailed means Google redirected back with an error other than access_denied.
	ErrAuthFailed = errors.New("googleauth: google returned an authorization error")

	// ErrInvalidClient means Google does not accept the client id and secret (invalid_client, unauthorized_client).
	ErrInvalidClient = errors.New("googleauth: google rejected the client id or secret")

	// ErrRejected means the token endpoint refused the request for another reason.
	ErrRejected = errors.New("googleauth: google refused the token request")

	// ErrUnavailable means Google answered with a server error or a rate limit.
	ErrUnavailable = errors.New("googleauth: google is temporarily unavailable")

	// ErrNetwork means Google could not be reached. It wraps shared.ErrNetworkUnavailable.
	ErrNetwork = fmt.Errorf("googleauth: google cannot be reached: %w", shared.ErrNetworkUnavailable)

	// ErrBadResponse means the token response could not be read.
	ErrBadResponse = errors.New("googleauth: unreadable token response")

	// ErrNoRefreshToken means the code exchange returned no refresh token; nothing was stored.
	ErrNoRefreshToken = errors.New("googleauth: google returned no refresh token")

	// ErrScope means the granted scopes are not exactly calendar.app.created; the grant was revoked and nothing was
	// stored. It wraps values.ErrCalendarScopeNotGranted or values.ErrScopeTooBroad.
	ErrScope = errors.New("googleauth: unexpected granted scope")

	// ErrReauthRequired means Google no longer accepts the stored refresh token (invalid_grant: revoked, expired, or
	// the password changed). The dead token was removed, so the app shows "not signed in".
	ErrReauthRequired = errors.New("googleauth: google sign-in expired, sign in again")

	// ErrRevokeFailed means sign-out removed the local tokens but Google did not confirm the revocation.
	ErrRevokeFailed = errors.New("googleauth: signed out locally, google did not confirm the revocation")
)

// oauthCode returns the OAuth error code of a token endpoint answer.
func oauthCode(err error) (string, bool) {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		return "", false
	}
	return re.ErrorCode, true
}

// tokenError classifies a failure of a token request. It never wraps the *oauth2.RetrieveError: its text holds the
// body of Google's answer.
func tokenError(ctx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", op, doneError(ctx))
	}
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		switch {
		case re.ErrorCode == "invalid_client" || re.ErrorCode == "unauthorized_client":
			return fmt.Errorf("%s: %w", op, ErrInvalidClient)
		case re.Response != nil && (re.Response.StatusCode >= 500 || re.Response.StatusCode == 429):
			return fmt.Errorf("%s: %w (status %d)", op, ErrUnavailable, re.Response.StatusCode)
		}
		return fmt.Errorf("%s: %w", op, ErrRejected)
	}
	if isNetwork(err) {
		return fmt.Errorf("%s: %w", op, ErrNetwork)
	}
	return fmt.Errorf("%s: %w", op, ErrBadResponse)
}

// isNetwork reports a transport failure of an HTTP call.
func isNetwork(err error) bool {
	var ue *url.Error
	var ne net.Error
	return errors.As(err, &ue) || errors.As(err, &ne)
}

// doneError is the error of a finished ctx: ErrTimeout when the sign-in bound fired, else the ctx error.
func doneError(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrTimeout) {
		return ErrTimeout
	}
	return ctx.Err()
}
