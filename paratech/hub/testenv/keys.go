package testenv

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// KeyStore is an in-memory API key repository with the semantics of the Postgres adapter: rows are stored as
// primitives and restored on every read, a duplicate id is shared.ErrAlreadyExists, a missing one shared.ErrNotFound,
// and a revocation is saved only once (apikey.ErrAlreadyRevoked for the loser of a race). Err, when set, fails
// every call.
type KeyStore struct {
	mu   sync.Mutex
	rows map[shared.APIKeyID]keyRow
	Err  error
}

type keyRow struct {
	label     string
	hash      []byte
	createdAt time.Time
	revokedAt time.Time
}

// NewKeyStore returns an empty store.
func NewKeyStore() *KeyStore {
	return &KeyStore{rows: map[shared.APIKeyID]keyRow{}}
}

// Create stores a new key.
func (s *KeyStore) Create(_ context.Context, k *apikey.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	if _, ok := s.rows[k.ID()]; ok {
		return fmt.Errorf("create %s: %w", k.ID(), shared.ErrAlreadyExists)
	}
	s.rows[k.ID()] = keyRow{label: k.Label().String(), hash: k.SecretHash(), createdAt: k.CreatedAt(), revokedAt: k.RevokedAt()}
	return nil
}

// Get loads a key.
func (s *KeyStore) Get(_ context.Context, id shared.APIKeyID) (*apikey.Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	r, ok := s.rows[id]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", id, shared.ErrNotFound)
	}
	return restore(id, r)
}

// SaveRevocation stores the revocation time of a revoked key.
func (s *KeyStore) SaveRevocation(_ context.Context, k *apikey.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	r, ok := s.rows[k.ID()]
	switch {
	case k.RevokedAt().IsZero():
		return fmt.Errorf("revoke %s: %w: the key is not revoked", k.ID(), apikey.ErrInvalid)
	case !ok:
		return fmt.Errorf("revoke %s: %w", k.ID(), shared.ErrNotFound)
	case !r.revokedAt.IsZero():
		return fmt.Errorf("revoke %s: %w", k.ID(), apikey.ErrAlreadyRevoked)
	}
	r.revokedAt = k.RevokedAt()
	s.rows[k.ID()] = r
	return nil
}

// List returns all keys ordered by creation time, then id.
func (s *KeyStore) List(context.Context) ([]*apikey.Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return nil, s.Err
	}
	out := make([]*apikey.Key, 0, len(s.rows))
	for id, r := range s.rows {
		k, err := restore(id, r)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt().Equal(out[j].CreatedAt()) {
			return out[i].CreatedAt().Before(out[j].CreatedAt())
		}
		return out[i].ID() < out[j].ID()
	})
	return out, nil
}

// Hashes returns every stored hash, so tests can check what the store holds.
func (s *KeyStore) Hashes() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]byte
	for _, r := range s.rows {
		out = append(out, r.hash)
	}
	return out
}

func restore(id shared.APIKeyID, r keyRow) (*apikey.Key, error) {
	l, err := apikey.NewLabel(r.label)
	if err != nil {
		return nil, err
	}
	return apikey.Restore(id, l, r.hash, r.createdAt, r.revokedAt)
}
