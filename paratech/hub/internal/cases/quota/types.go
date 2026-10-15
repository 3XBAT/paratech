package quota

import (
	"time"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

// Config is the quota configuration of the Hub (ADR-0008).
type Config struct {
	Unit           string        // quota unit; only "tokens"
	Limit          int64         // allowance of every tester
	ReservationTTL time.Duration // how long an unsettled reservation holds quota; above the gateway request timeout
	KeyRetention   time.Duration // how long an idempotency key record lives
}

// ReserveCommand holds an amount before a provider call. Owner is the authenticated API key.
type ReserveCommand struct {
	Owner          shared.APIKeyID
	IdempotencyKey string
	Operation      string // account.Operation values
	ModelAlias     string
	Unit           string
	Amount         int64
}

// SettleCommand charges the actual usage of a reservation of Owner.
type SettleCommand struct {
	Owner         shared.APIKeyID
	ReservationID string
	Unit          string
	Actual        int64
	Partial       bool
}

// ReleaseCommand returns a reservation of Owner whole.
type ReleaseCommand struct {
	Owner         shared.APIKeyID
	ReservationID string
}

// Reservation describes one reservation as the contract reports it.
type Reservation struct {
	ID        string
	State     string // account.State values: reserved, settled, released
	Unit      string
	Reserved  int64
	Charged   int64
	ExpiresAt time.Time
}

// Snapshot is the quota of a tester at one moment; it does not renew in MVP (no period end).
type Snapshot struct {
	Unit      string
	Remaining int64
	Reserved  int64
	Limit     int64
}

// Result is the outcome of Reserve, Settle and Release.
type Result struct {
	Reservation Reservation
	Quota       Snapshot
	// Replayed is true when Reserve returned an existing reservation of the key instead of holding anything.
	Replayed bool
}

func snapshotOf(s account.Snapshot) Snapshot {
	return Snapshot{Unit: string(s.Unit), Remaining: s.Remaining, Reserved: s.Reserved, Limit: s.Limit}
}

func reservationOf(r account.ReservationRecord, unit account.Unit) Reservation {
	return Reservation{
		ID: r.ID.String(), State: string(r.State), Unit: string(unit),
		Reserved: r.Amount, Charged: r.Charged, ExpiresAt: r.ExpiresAt,
	}
}
