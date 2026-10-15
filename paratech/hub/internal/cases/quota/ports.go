package quota

import (
	"context"
	"time"

	"paratech/hub/internal/domain/quota/account"
)

// Accounts stores quota accounts. Implemented by the postgres adapter. A future gateway use case declares its own
// port with the same shape (ARCH-20 forbids importing this package) and the same adapter satisfies both.
type Accounts interface {
	// Modify runs fn on the account of scope.Owner, loaded as account.Scope describes, inside one transaction that
	// serializes every command of that owner. If fn returns nil, the account's Changes (totals, reservations, key
	// records, ledger movements) are stored atomically; if it returns an error, nothing is stored and the error is
	// returned unchanged. An owner without an account starts with an empty one.
	Modify(ctx context.Context, scope account.Scope, fn func(*account.Account) error) error

	// Read loads the account of scope.Owner (as Scope describes, without Key and Reservation) from one consistent
	// snapshot, without locking or writing. An owner without an account gets an empty one.
	Read(ctx context.Context, scope account.Scope) (*account.Account, error)
}

// Clock supplies the time of a command (ARCH-24).
type Clock interface {
	Now() time.Time
}

// Random fills p with cryptographically secure random bytes for reservation ids (ARCH-24).
type Random interface {
	Fill(p []byte) error
}
