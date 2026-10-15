package account

import (
	"fmt"
	"time"

	"paratech/hub/internal/domain/shared"
)

// MaxExpiredPerLoad bounds how many overdue reservations a repository loads for one command (oldest first). The rest
// are released by the following commands; in practice a tester has a handful of open reservations.
const MaxExpiredPerLoad = 100

// Policy is the quota configuration of every account (ADR-0008): one non-renewing allowance per tester, applied when
// the account is loaded, so a new limit applies to everyone at once.
type Policy struct {
	unit      Unit
	limit     int64
	ttl       time.Duration
	retention time.Duration
}

// NewPolicy validates the configuration: a known unit, a positive limit and positive durations; a key record must
// live at least as long as a reservation, so that a live reservation always has its key.
func NewPolicy(unit string, limit int64, reservationTTL, keyRetention time.Duration) (Policy, error) {
	u, err := ParseUnit(unit)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: %w", ErrInvalidPolicy, err)
	}
	switch {
	case limit <= 0:
		return Policy{}, fmt.Errorf("%w: limit must be positive", ErrInvalidPolicy)
	case reservationTTL <= 0:
		return Policy{}, fmt.Errorf("%w: reservation TTL must be positive", ErrInvalidPolicy)
	case keyRetention < reservationTTL:
		return Policy{}, fmt.Errorf("%w: key retention must not be shorter than the reservation TTL", ErrInvalidPolicy)
	}
	return Policy{unit: u, limit: limit, ttl: reservationTTL, retention: keyRetention}, nil
}

// Unit returns the quota unit.
func (p Policy) Unit() Unit { return p.unit }

// Limit returns the allowance of every tester.
func (p Policy) Limit() int64 { return p.limit }

// ReservationTTL returns how long an unsettled reservation holds quota.
func (p Policy) ReservationTTL() time.Duration { return p.ttl }

// KeyRetention returns how long an idempotency key record lives; after that the same text is a new key.
func (p Policy) KeyRetention() time.Duration { return p.retention }

func (p Policy) valid() bool {
	return p.unit == UnitTokens && p.limit > 0 && p.ttl > 0 && p.retention >= p.ttl
}

// Scope tells a repository which account to build and which reservations one command needs (the aggregate is loaded
// partially, ADR-0008):
//   - the RESERVED reservations of Owner with expires_at <= Now, oldest first, at most MaxExpiredPerLoad;
//   - when Key is set, its key record and the reservation of its latest attempt;
//   - when Reservation is set, that reservation if it belongs to Owner.
type Scope struct {
	Owner       shared.APIKeyID
	Policy      Policy
	Now         time.Time
	Key         IdempotencyKey
	Reservation shared.ReservationID
}
