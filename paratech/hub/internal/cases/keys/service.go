// Package keys is the use case of the key-management CLI (PRD section 12, AC-25): issue, revoke and list per-tester
// Hub API keys. The plaintext key leaves this package exactly once, in the result of Issue.
package keys

import (
	"context"
	"errors"
	"fmt"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// Operation names used in error messages.
const (
	opIssue  = "issue"
	opRevoke = "revoke"
	opList   = "list"
)

// Service issues, revokes and lists API keys. Every method is one repository write at most (ARCH-23).
type Service struct {
	repo   KeyRepository
	clock  Clock
	random Random
}

// New builds the Service.
func New(repo KeyRepository, clock Clock, random Random) (*Service, error) {
	if repo == nil || clock == nil || random == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	return &Service{repo: repo, clock: clock, random: random}, nil
}

// Issue creates a key for label and returns it with the plaintext key, which the caller must show once.
func (s *Service) Issue(ctx context.Context, label string) (Issued, error) {
	l, err := apikey.NewLabel(label)
	if err != nil {
		return Issued{}, fmt.Errorf("keys: %s: %w", opIssue, err)
	}

	idBytes, secretBytes := make([]byte, apikey.IDBytes), make([]byte, apikey.SecretBytes)
	defer clear(secretBytes)
	if err := s.random.Fill(idBytes); err != nil {
		return Issued{}, fmt.Errorf("keys: %s: random id: %w", opIssue, err)
	}
	if err := s.random.Fill(secretBytes); err != nil {
		return Issued{}, fmt.Errorf("keys: %s: random secret: %w", opIssue, err)
	}
	cred, err := apikey.NewCredential(idBytes, secretBytes)
	if err != nil {
		return Issued{}, fmt.Errorf("keys: %s: %w", opIssue, err)
	}

	k, err := apikey.New(cred, l, s.clock.Now())
	if err != nil {
		return Issued{}, fmt.Errorf("keys: %s: %w", opIssue, err)
	}
	if err := s.repo.Create(ctx, k); err != nil {
		return Issued{}, fmt.Errorf("keys: %s: %w", opIssue, err)
	}
	return Issued{KeyInfo: info(k), Key: cred.Reveal()}, nil
}

// Revoke revokes the key with the given id and returns its state. Revoking a revoked key returns its state with an
// error that matches apikey.ErrAlreadyRevoked; the stored revocation time is kept.
func (s *Service) Revoke(ctx context.Context, id string) (KeyInfo, error) {
	kid := shared.APIKeyID(id)
	if err := kid.Validate(); err != nil {
		return KeyInfo{}, fmt.Errorf("keys: %s: %w", opRevoke, err)
	}

	k, err := s.repo.Get(ctx, kid)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("keys: %s: %w", opRevoke, err)
	}
	if err := k.Revoke(s.clock.Now()); err != nil {
		return info(k), fmt.Errorf("keys: %s: %w", opRevoke, err)
	}

	err = s.repo.SaveRevocation(ctx, k)
	if errors.Is(err, apikey.ErrAlreadyRevoked) {
		// A concurrent revocation won; report the time it stored.
		if stored, getErr := s.repo.Get(ctx, kid); getErr == nil {
			k = stored
		}
	}
	if err != nil {
		return info(k), fmt.Errorf("keys: %s: %w", opRevoke, err)
	}
	return info(k), nil
}

// List returns every key without secrets or hashes, oldest first.
func (s *Service) List(ctx context.Context) ([]KeyInfo, error) {
	all, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("keys: %s: %w", opList, err)
	}
	out := make([]KeyInfo, 0, len(all))
	for _, k := range all {
		out = append(out, info(k))
	}
	return out, nil
}
