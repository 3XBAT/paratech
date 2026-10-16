package googlesignin

import (
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/schedule/values"
	"paratech/desktop/internal/domain/shared"
)

// Errors of the use case (ARCH-18).
var (
	// ErrInvalidConfig is returned by New when a port is missing.
	ErrInvalidConfig = errors.New("invalid google sign-in configuration")

	// ErrInvalidClientID means the value is not a Google client id (values.ParseGoogleClientID).
	ErrInvalidClientID = fmt.Errorf("googlesignin: %w", values.ErrInvalidGoogleClientID)

	// ErrInvalidClientSecret means the value cannot be a client secret. It wraps shared.ErrInvalidSecret.
	ErrInvalidClientSecret = fmt.Errorf("googlesignin: %w", values.ErrInvalidGoogleClientSecret)

	// ErrSecretsUnsupported means there is no credential store on this system. It wraps errors.ErrUnsupported.
	ErrSecretsUnsupported = fmt.Errorf("googlesignin: credential store not supported: %w", errors.ErrUnsupported)

	// ErrSecretsUnavailable means the credential store failed.
	ErrSecretsUnavailable = errors.New("googlesignin: credential store unavailable")

	// ErrSignedIn means the client cannot change while signed in: sign out first.
	ErrSignedIn = errors.New("googlesignin: signed in to google, sign out first")

	// ErrSignInInProgress means a sign-in or sign-out is already running.
	ErrSignInInProgress = errors.New("googlesignin: a google sign-in is already running")
)

// Results of SignIn and SignOut other than nil. The Authorizer returns those of Google and the browser; the page shows
// a fixed text for each.
var (
	ErrClientNotConfigured = errors.New("googlesignin: the google client id and secret are not set")
	ErrSignInCancelled     = errors.New("googlesignin: the sign-in was cancelled")
	ErrSignInTimeout       = errors.New("googlesignin: the sign-in was not finished in time")
	ErrBrowserUnavailable  = errors.New("googlesignin: the browser could not be opened")
	ErrAccessDenied        = errors.New("googlesignin: the user denied access")
	ErrScopeNotGranted     = errors.New("googlesignin: access to the app calendars was not granted")
	ErrScopeTooBroad       = errors.New("googlesignin: google granted more access than requested")
	ErrNoRefreshToken      = errors.New("googlesignin: google granted no long-lived access")
	ErrInvalidClient       = errors.New("googlesignin: google rejected the client id or secret")
	ErrGoogleRefused       = errors.New("googlesignin: google refused the sign-in")
	ErrGoogleUnreachable   = errors.New("googlesignin: google cannot be reached")
	ErrRevokeFailed        = errors.New("googlesignin: signed out locally, google did not confirm the revocation")
)

// results are the classified results of the Authorizer; anything else is wrapped as unclassified.
var results = []error{
	ErrClientNotConfigured, ErrSignInTimeout, ErrBrowserUnavailable, ErrAccessDenied, ErrScopeNotGranted,
	ErrScopeTooBroad, ErrNoRefreshToken, ErrInvalidClient, ErrGoogleRefused, ErrGoogleUnreachable, ErrRevokeFailed,
	ErrSecretsUnsupported, ErrSecretsUnavailable,
}

// storeFailure classifies an error of the credential store. The store's errors carry no secret.
func storeFailure(err error) (SecretStatus, error) {
	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return SecretUnsupported, ErrSecretsUnsupported
	case errors.Is(err, shared.ErrInvalidSecret):
		return SecretUnavailable, ErrInvalidClientSecret
	}
	return SecretUnavailable, fmt.Errorf("%w: %w", ErrSecretsUnavailable, err)
}
