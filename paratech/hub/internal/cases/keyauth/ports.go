package keyauth

import (
	"context"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
)

// KeyFinder loads API keys. Implemented by the postgres adapter.
type KeyFinder interface {
	// Get loads a key; a missing one is shared.ErrNotFound.
	Get(ctx context.Context, id shared.APIKeyID) (*apikey.Key, error)
}
