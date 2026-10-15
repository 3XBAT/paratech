package account

import (
	"time"

	"paratech/hub/internal/domain/shared"
)

// MovementKind names a quota movement of the ledger.
type MovementKind string

// Movement kinds.
const (
	MovementReserve MovementKind = "reserve" // an amount is held
	MovementSettle  MovementKind = "settle"  // the hold (if any) is returned and the actual usage charged
	MovementRelease MovementKind = "release" // the hold is returned by Release or after a failed call
	MovementExpire  MovementKind = "expire"  // the hold is returned by the Hub at expires_at
)

// Movement is a ledger entry produced by the aggregate (ARCH-16): one per change of the account totals. The sums of
// HeldDelta and UsedDelta over all movements of an account are its held and used totals.
type Movement struct {
	Kind        MovementKind
	Reservation shared.ReservationID
	HeldDelta   int64
	UsedDelta   int64
	At          time.Time
}
