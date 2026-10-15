package keys

import (
	"context"
	"time"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// KeyRepository stores API keys. Implemented by the postgres adapter.
type KeyRepository interface {
	// Create stores a new key; an existing id is shared.ErrAlreadyExists.
	Create(ctx context.Context, k *apikey.Key) error

	// Get loads a key; a missing one is shared.ErrNotFound.
	Get(ctx context.Context, id shared.APIKeyID) (*apikey.Key, error)

	// SaveRevocation records a revocation only over an active stored key: a lost race is apikey.ErrAlreadyRevoked,
	// a missing key shared.ErrNotFound.
	SaveRevocation(ctx context.Context, k *apikey.Key) error

	// List returns every key ordered by creation time.
	List(ctx context.Context) ([]*apikey.Key, error)
}

// Clock supplies the issue and revocation times (ARCH-24).
type Clock interface {
	Now() time.Time
}

// Random fills p with cryptographically secure random bytes (ARCH-24). Implemented by the random adapter.
type Random interface {
	Fill(p []byte) error
}
