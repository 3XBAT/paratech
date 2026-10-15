// Package keyauth is the use case that authenticates a Hub request by its per-tester API key (PRD section 12,
// AC-25). It is called by the transport for every RPC before any handler runs.
package keyauth

import (
	"context"
	"errors"
	"fmt"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// Service checks presented API keys.
type Service struct {
	keys KeyFinder
}

// New builds the Service.
func New(keys KeyFinder) (*Service, error) {
	if keys == nil {
		return nil, fmt.Errorf("%w: the key finder is required", ErrInvalidConfig)
	}
	return &Service{keys: keys}, nil
}

// Authenticate returns the id of the key if presented is a valid, active key. Every rejection matches
// ErrInvalidKey; any other error is a failure of the key store, not a verdict on the key. The format is checked
// before the store is touched, and an unknown id costs the same hash comparison as a wrong secret.
func (s *Service) Authenticate(ctx context.Context, presented string) (shared.APIKeyID, error) {
	cred, err := apikey.ParseCredential(presented)
	if err != nil {
		_ = apikey.VerifyMissing(apikey.Credential{})
		return "", rejected(err)
	}

	k, err := s.keys.Get(ctx, cred.ID())
	if errors.Is(err, shared.ErrNotFound) {
		return "", rejected(apikey.VerifyMissing(cred))
	}
	if err != nil {
		return "", fmt.Errorf("keyauth: look up key: %w", err)
	}

	if err := k.Verify(cred); err != nil {
		return "", rejected(err)
	}
	return k.ID(), nil
}

func rejected(reason error) error {
	return fmt.Errorf("%w: %w", ErrInvalidKey, reason)
}
