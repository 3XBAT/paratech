package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/adapters/googleauth"
	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/googlesignin"
	"paratech/desktop/internal/domain/schedule/values"
)

// The bridge satisfies the port of the use case, and the adapter is what it calls.
var (
	_ googlesignin.Authorizer = googleSignIn{}
	_ googleAuthorizer        = (*googleauth.Authorizer)(nil)
)

// wireGoogle builds the Google section of the «Подключения» page (TASK-050): the googleauth adapter on the google
// credential store (it reads the client and keeps the refresh token there) and the browser, and the googlesignin use
// case on the same store (writes and existence only), which reaches the adapter through the bridge below. A successful
// sign-in asks for a calendar sync. The Calendar adapter (TASK-051) authorizes with c.google and keeps the id of the
// app calendar in the same store; its base URL is the adapter's default, only tests replace it.
//
// Sign-out keeps the calendar id: the same account signing in again gets its calendar back (no second one), and
// another account cannot see it (calendars.get and calendarList.get both refuse), so the adapter makes a fresh
// calendar and the sync rewrites every lesson into it.
func (c *core) wireGoogle(e env, profile wincred.Profile, log *slog.Logger) error {
	secrets, err := e.secrets(profile, googleSecretsService)
	if err != nil {
		return fmt.Errorf("app: google credentials: %w", err)
	}
	c.google, err = googleauth.New(secrets, e.authPages, e.googleOAuth...)
	if err != nil {
		return fmt.Errorf("app: google authorization: %w", err)
	}
	c.calendar, err = googlecal.New(c.google, secrets, classifyGoogleAuth, e.googleCalendar...)
	if err != nil {
		return fmt.Errorf("app: google calendar: %w", err)
	}
	// The hook reads c.calendarSync when it runs: the sync is built after the sign-in it depends on.
	bridge := googleSignIn{auth: c.google, signedIn: func(ctx context.Context) { c.requestCalendarSync(ctx, log, "sign-in") }}
	c.googleSignIn, err = googlesignin.New(googlesignin.Deps{Secrets: secrets, Auth: bridge, Log: log},
		googlesignin.Config{
			ClientIDName: googleauth.ClientIDName, ClientSecretName: googleauth.ClientSecretName,
			RefreshTokenName: googleauth.RefreshTokenName,
		})
	if err != nil {
		return fmt.Errorf("app: google sign-in: %w", err)
	}
	return nil
}

// googleAuthorizer is googleauth.Authorizer as the bridge uses it.
type googleAuthorizer interface {
	SignIn(ctx context.Context) error
	SignOut(ctx context.Context) error
}

// googleSignIn is the googlesignin.Authorizer of the googleauth adapter: mapping only, plus signedIn after a successful
// sign-in (nil: nothing). The adapter's texts are fixed and carry no token, code or secret, so the cause stays wrapped.
type googleSignIn struct {
	auth     googleAuthorizer
	signedIn func(ctx context.Context)
}

func (g googleSignIn) SignIn(ctx context.Context) error {
	if err := g.auth.SignIn(ctx); err != nil {
		return googleResult(err)
	}
	if g.signedIn != nil {
		g.signedIn(ctx)
	}
	return nil
}

func (g googleSignIn) SignOut(ctx context.Context) error { return googleResult(g.auth.SignOut(ctx)) }

// googleResult maps the adapter's classes to the results of the use case. Store failures come first: after a failed
// sign-out they mean the refresh token may still be stored, which matters more than the revocation.
func googleResult(err error) error {
	var result error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, googleauth.ErrCredentials) && errors.Is(err, errors.ErrUnsupported):
		result = googlesignin.ErrSecretsUnsupported
	case errors.Is(err, googleauth.ErrCredentials):
		result = googlesignin.ErrSecretsUnavailable
	case errors.Is(err, googleauth.ErrNotConfigured):
		result = googlesignin.ErrClientNotConfigured
	case errors.Is(err, googleauth.ErrRevokeFailed):
		result = googlesignin.ErrRevokeFailed
	case errors.Is(err, googleauth.ErrTimeout):
		result = googlesignin.ErrSignInTimeout
	case errors.Is(err, googleauth.ErrBrowser):
		result = googlesignin.ErrBrowserUnavailable
	case errors.Is(err, googleauth.ErrDenied):
		result = googlesignin.ErrAccessDenied
	case errors.Is(err, googleauth.ErrScope) && errors.Is(err, values.ErrCalendarScopeNotGranted):
		result = googlesignin.ErrScopeNotGranted
	case errors.Is(err, googleauth.ErrScope):
		result = googlesignin.ErrScopeTooBroad
	case errors.Is(err, googleauth.ErrNoRefreshToken):
		result = googlesignin.ErrNoRefreshToken
	case errors.Is(err, googleauth.ErrInvalidClient):
		result = googlesignin.ErrInvalidClient
	case errors.Is(err, googleauth.ErrAuthFailed), errors.Is(err, googleauth.ErrRejected),
		errors.Is(err, googleauth.ErrBadResponse):
		result = googlesignin.ErrGoogleRefused
	case errors.Is(err, googleauth.ErrNetwork), errors.Is(err, googleauth.ErrUnavailable):
		result = googlesignin.ErrGoogleUnreachable
	default:
		return err // a ctx error or the loopback listener: not a result of Google
	}
	return fmt.Errorf("%w: %w", result, err)
}
