package googlesignin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/schedule/values"
)

// Operation names, for error texts and logs.
const (
	opStatus      = "googlesignin: status"
	opSetClient   = "googlesignin: set client"
	opClearClient = "googlesignin: clear client"
	opSignIn      = "googlesignin: sign in"
	opSignOut     = "googlesignin: sign out"
)

// Service runs the Google section. It is safe for concurrent use.
type Service struct {
	secrets SecretWriter
	auth    Authorizer
	names   Config
	log     *slog.Logger

	// mu guards the running operation: busy while a sign-in or sign-out runs; cancel only while a sign-in runs.
	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
}

// New checks the ports.
func New(d Deps, cfg Config) (*Service, error) {
	if d.Secrets == nil || d.Auth == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if cfg.ClientIDName == "" {
		cfg.ClientIDName = DefaultClientIDName
	}
	if cfg.ClientSecretName == "" {
		cfg.ClientSecretName = DefaultClientSecretName
	}
	if cfg.RefreshTokenName == "" {
		cfg.RefreshTokenName = DefaultRefreshTokenName
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{secrets: d.Secrets, auth: d.Auth, names: cfg, log: log}, nil
}

// Status returns what is stored and whether a sign-in waits for the browser. A store failure is a status, not an
// error. It does not call Google.
func (s *Service) Status(ctx context.Context) Status {
	s.mu.Lock()
	signingIn := s.cancel != nil
	s.mu.Unlock()
	return Status{Client: s.clientStatus(ctx), SignedIn: s.status(ctx, s.names.RefreshTokenName), SigningIn: signingIn}
}

// SetClient stores or replaces the client id and secret of the user's Google Cloud Desktop client. Both are checked
// first (surrounding blanks of a paste are dropped); then nothing changes on ErrInvalidClientID or
// ErrInvalidClientSecret. While signed in the client cannot change (ErrSignedIn). It returns the status afterwards,
// never a value.
func (s *Service) SetClient(ctx context.Context, clientID, clientSecret string) (Status, error) {
	id, err := values.ParseGoogleClientID(clientID)
	if err != nil {
		return s.Status(ctx), fmt.Errorf("%s: %w", opSetClient, ErrInvalidClientID)
	}
	secret, err := values.ParseGoogleClientSecret(clientSecret)
	if err != nil {
		return s.Status(ctx), fmt.Errorf("%s: %w", opSetClient, ErrInvalidClientSecret)
	}
	err = s.whileIdle(ctx, opSetClient, func() error {
		if err := s.secrets.Set(ctx, s.names.ClientSecretName, secret); err != nil {
			return err
		}
		return s.secrets.Set(ctx, s.names.ClientIDName, id)
	})
	return s.Status(ctx), err
}

// ClearClient removes the client id and secret; missing ones are not an error. While signed in it is refused
// (ErrSignedIn): sign out first, so the grant is revoked with the client that obtained it.
func (s *Service) ClearClient(ctx context.Context) (Status, error) {
	err := s.whileIdle(ctx, opClearClient, func() error {
		if err := s.secrets.Delete(ctx, s.names.ClientIDName); err != nil {
			return err
		}
		return s.secrets.Delete(ctx, s.names.ClientSecretName)
	})
	return s.Status(ctx), err
}

// whileIdle runs a change of the client when no sign-in or sign-out runs and nobody is signed in.
func (s *Service) whileIdle(ctx context.Context, op string, change func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("%s: %w", op, ErrSignInInProgress)
	}
	signedIn, err := s.secrets.Exists(ctx, s.names.RefreshTokenName)
	if err != nil {
		_, cause := storeFailure(err)
		return fmt.Errorf("%s: %w", op, cause)
	}
	if signedIn {
		return fmt.Errorf("%s: %w", op, ErrSignedIn)
	}
	if err := change(); err != nil {
		_, cause := storeFailure(err)
		return fmt.Errorf("%s: %w", op, cause)
	}
	return nil
}

// SignIn runs the browser sign-in and returns when it ends: nil once the refresh token is stored, ErrSignInInProgress
// while another sign-in or sign-out runs, ErrClientNotConfigured without a client, ErrSignInCancelled after
// CancelSignIn or when ctx ends, or another result of this package. A new sign-in replaces the stored grant.
func (s *Service) SignIn(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.begin(ctx, cancel); err != nil {
		return err
	}
	defer s.end()

	err := s.auth.SignIn(ctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		// CancelSignIn, or the caller's ctx ended (the page went away); the sign-in bound is ErrSignInTimeout.
		return fmt.Errorf("%s: %w", opSignIn, ErrSignInCancelled)
	}
	return result(opSignIn, err)
}

// begin marks a sign-in as running after checking that the client is stored.
func (s *Service) begin(ctx context.Context, cancel context.CancelFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("%s: %w", opSignIn, ErrSignInInProgress)
	}
	switch status := s.clientStatus(ctx); status {
	case SecretSet:
	case SecretUnset:
		return fmt.Errorf("%s: %w", opSignIn, ErrClientNotConfigured)
	case SecretUnsupported:
		return fmt.Errorf("%s: %w", opSignIn, ErrSecretsUnsupported)
	default:
		return fmt.Errorf("%s: %w", opSignIn, ErrSecretsUnavailable)
	}
	s.busy, s.cancel = true, cancel
	return nil
}

func (s *Service) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy, s.cancel = false, nil
}

// CancelSignIn stops the running sign-in, which then returns ErrSignInCancelled. It reports whether one was running.
func (s *Service) CancelSignIn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}

// SignOut revokes the grant and removes the refresh token; the client stays. It returns nil, ErrRevokeFailed (signed
// out locally, the user should revoke the access in the Google account), ErrSignInInProgress while a sign-in runs, or
// another result of this package. Without a stored token it only clears.
func (s *Service) SignOut(ctx context.Context) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return fmt.Errorf("%s: %w", opSignOut, ErrSignInInProgress)
	}
	s.busy = true
	s.mu.Unlock()
	defer s.end()

	if err := s.auth.SignOut(ctx); err != nil {
		return result(opSignOut, err)
	}
	return nil
}

// result keeps a classified result of the Authorizer and wraps anything else.
func result(op string, err error) error {
	for _, known := range results {
		if errors.Is(err, known) {
			return err
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// clientStatus is SecretSet when both client values are stored, SecretUnset when either is missing.
func (s *Service) clientStatus(ctx context.Context) SecretStatus {
	id := s.status(ctx, s.names.ClientIDName)
	secret := s.status(ctx, s.names.ClientSecretName)
	switch {
	case id == SecretSet && secret == SecretSet:
		return SecretSet
	case id == SecretUnsupported || secret == SecretUnsupported:
		return SecretUnsupported
	case id == SecretUnavailable || secret == SecretUnavailable:
		return SecretUnavailable
	}
	return SecretUnset
}

// status asks the store whether name exists; a failure is logged and becomes a status.
func (s *Service) status(ctx context.Context, name string) SecretStatus {
	ok, err := s.secrets.Exists(ctx, name)
	if err != nil {
		status, cause := storeFailure(err)
		s.log.Warn(opStatus, "entry", name, "err", cause)
		return status
	}
	if ok {
		return SecretSet
	}
	return SecretUnset
}
