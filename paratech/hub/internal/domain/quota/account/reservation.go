package account

import (
	"encoding/hex"
	"fmt"
	"time"

	"paratech/hub/internal/domain/shared"
)

// ReservationIDBytes is the number of random bytes of a reservation id.
const ReservationIDBytes = 16

// NewReservationID builds an id from random bytes supplied by the caller (ARCH-15). All-zero bytes are rejected as
// a sign of a broken random source.
func NewReservationID(random []byte) (shared.ReservationID, error) {
	if len(random) != ReservationIDBytes {
		return "", fmt.Errorf("%w: want %d random bytes for a reservation id", ErrInvalidArgument, ReservationIDBytes)
	}
	zero := true
	for _, b := range random {
		zero = zero && b == 0
	}
	if zero {
		return "", fmt.Errorf("%w: all-zero reservation id", ErrInvalidArgument)
	}
	return shared.ReservationID(hex.EncodeToString(random)), nil
}

// State is the contract state of a reservation.
type State string

// States.
const (
	StateReserved State = "reserved"
	StateSettled  State = "settled"
	StateReleased State = "released"
)

// ReleaseReason says why a reservation was released. It is internal to the Hub (ADR-0005(d)).
type ReleaseReason string

// Release reasons; a reservation that is not RELEASED has none.
const (
	ReasonNone     ReleaseReason = ""
	ReasonExplicit ReleaseReason = "explicit" // Release was called
	ReasonFailed   ReleaseReason = "failed"   // the call failed before the provider answered
	ReasonExpired  ReleaseReason = "expired"  // released by the Hub at expires_at
)

// phase is a state with its release reason: the unit of the transition table.
type phase string

const (
	phaseInvalid          phase = ""
	phaseReserved         phase = "reserved"
	phaseSettled          phase = "settled"
	phaseReleasedExplicit phase = "released/explicit"
	phaseReleasedFailed   phase = "released/failed"
	phaseReleasedExpired  phase = "released/expired"
)

// phaseOf combines a state and a reason; a combination that cannot exist is phaseInvalid.
func phaseOf(s State, r ReleaseReason) phase {
	switch {
	case s == StateReserved && r == ReasonNone:
		return phaseReserved
	case s == StateSettled && r == ReasonNone:
		return phaseSettled
	case s == StateReleased && r == ReasonExplicit:
		return phaseReleasedExplicit
	case s == StateReleased && r == ReasonFailed:
		return phaseReleasedFailed
	case s == StateReleased && r == ReasonExpired:
		return phaseReleasedExpired
	default:
		return phaseInvalid
	}
}

// parts splits a phase into its state and reason.
func (p phase) parts() (State, ReleaseReason) {
	switch p {
	case phaseReserved:
		return StateReserved, ReasonNone
	case phaseSettled:
		return StateSettled, ReasonNone
	case phaseReleasedExplicit:
		return StateReleased, ReasonExplicit
	case phaseReleasedFailed:
		return StateReleased, ReasonFailed
	case phaseReleasedExpired:
		return StateReleased, ReasonExpired
	default:
		return "", ReasonNone
	}
}

// canMove is the transition table of a reservation (ARCH-19): RESERVED -> SETTLED | RELEASED(any reason), and
// RELEASED(expired) -> SETTLED once, for a late Settle. Everything else is final.
func canMove(from, to phase) bool {
	switch from {
	case phaseReserved:
		return to == phaseSettled || to == phaseReleasedExplicit || to == phaseReleasedFailed || to == phaseReleasedExpired
	case phaseReleasedExpired:
		return to == phaseSettled
	default:
		return false
	}
}

// ReservationRecord is the stored and reported form of a reservation (a copy; changing it changes nothing in the
// aggregate). Actual and Partial are what Settle was called with, kept so a repeated Settle can be compared.
type ReservationRecord struct {
	ID        shared.ReservationID
	Key       IdempotencyKey
	Attempt   int
	Amount    int64 // held while RESERVED; stays the reserved amount in every state
	State     State
	Reason    ReleaseReason
	Charged   int64
	Actual    int64
	Partial   bool
	CreatedAt time.Time
	ExpiresAt time.Time
	ClosedAt  time.Time // time of the last transition; zero while RESERVED
}

// reservation is an entity of the account aggregate; only Account changes it.
type reservation struct {
	rec ReservationRecord
}

func (r *reservation) phase() phase { return phaseOf(r.rec.State, r.rec.Reason) }

// moveTo applies one transition of the table.
func (r *reservation) moveTo(to phase, now time.Time) error {
	if !canMove(r.phase(), to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, r.phase(), to)
	}
	r.rec.State, r.rec.Reason = to.parts()
	r.rec.ClosedAt = now
	return nil
}

// overdue reports whether a RESERVED reservation has reached its expiry at now.
func (r *reservation) overdue(now time.Time) bool {
	return r.rec.State == StateReserved && !now.Before(r.rec.ExpiresAt)
}

// restoreReservation checks a stored reservation.
func restoreReservation(rec ReservationRecord) (*reservation, error) {
	bad := func(why string) (*reservation, error) {
		return nil, fmt.Errorf("%w: reservation: %s", ErrInvalidRecord, why)
	}
	if rec.ID.Validate() != nil {
		return bad("invalid id")
	}
	if _, err := ParseIdempotencyKey(string(rec.Key)); err != nil {
		return bad("invalid idempotency key")
	}
	switch {
	case rec.Attempt < 1:
		return bad("attempt must be positive")
	case rec.Amount <= 0:
		return bad("amount must be positive")
	case rec.Charged < 0 || rec.Actual < 0 || rec.Charged > rec.Actual:
		return bad("charged must be within 0..actual")
	case rec.CreatedAt.IsZero() || !rec.ExpiresAt.After(rec.CreatedAt):
		return bad("expiry must follow creation")
	}

	open := rec.Charged == 0 && rec.Actual == 0 && !rec.Partial
	switch phaseOf(rec.State, rec.Reason) {
	case phaseReserved:
		if !open || !rec.ClosedAt.IsZero() {
			return bad("a reserved reservation has no settlement and no close time")
		}
	case phaseSettled:
		if rec.ClosedAt.IsZero() {
			return bad("a settled reservation has a close time")
		}
	case phaseReleasedExplicit, phaseReleasedFailed, phaseReleasedExpired:
		if !open || rec.ClosedAt.IsZero() {
			return bad("a released reservation has no settlement and has a close time")
		}
	default:
		return bad("unknown state")
	}
	return &reservation{rec: rec}, nil
}
