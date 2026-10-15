package values

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidQuota is returned for a quota amount or snapshot that breaks its rules.
var ErrInvalidQuota = errors.New("invalid quota")

// QuotaUnit is the unit of a quota amount (PRD section 9.2; the final unit is open, section 19).
type QuotaUnit string

// Quota units.
const (
	QuotaUnitTokens QuotaUnit = "tokens" // provider tokens, input and output together
)

// QuotaAmount is an exact, non-negative quantity of quota. The zero value is not an amount.
type QuotaAmount struct {
	unit  QuotaUnit
	value int64
}

// NewQuotaAmount checks the unit and that value is not negative.
func NewQuotaAmount(unit QuotaUnit, value int64) (QuotaAmount, error) {
	if unit != QuotaUnitTokens {
		return QuotaAmount{}, fmt.Errorf("%w: unknown unit", ErrInvalidQuota)
	}
	if value < 0 {
		return QuotaAmount{}, fmt.Errorf("%w: negative amount", ErrInvalidQuota)
	}
	return QuotaAmount{unit: unit, value: value}, nil
}

// Unit returns the unit.
func (a QuotaAmount) Unit() QuotaUnit { return a.unit }

// Value returns the number of units.
func (a QuotaAmount) Value() int64 { return a.value }

// QuotaSnapshot is the tester's quota as the Hub reported it (AC-12). The Hub is the authority: a snapshot is only
// shown, never used to decide whether a request may run. The zero value is "no snapshot".
type QuotaSnapshot struct {
	remaining QuotaAmount
	reserved  QuotaAmount
	limit     QuotaAmount
	periodEnd time.Time // zero: the quota does not renew
}

// NewQuotaSnapshot checks that every amount is an amount of one unit and that remaining does not exceed limit
// (remaining is the limit minus the used and held parts, never below zero). Reserved may exceed the limit when the
// limit was lowered under running requests. A zero periodEnd means the quota does not renew.
func NewQuotaSnapshot(remaining, reserved, limit QuotaAmount, periodEnd time.Time) (QuotaSnapshot, error) {
	for _, a := range []QuotaAmount{remaining, reserved, limit} {
		if a == (QuotaAmount{}) {
			return QuotaSnapshot{}, fmt.Errorf("%w: missing amount", ErrInvalidQuota)
		}
	}
	if remaining.unit != limit.unit || reserved.unit != limit.unit {
		return QuotaSnapshot{}, fmt.Errorf("%w: amounts of different units", ErrInvalidQuota)
	}
	if remaining.value > limit.value {
		return QuotaSnapshot{}, fmt.Errorf("%w: remaining above the limit", ErrInvalidQuota)
	}
	return QuotaSnapshot{remaining: remaining, reserved: reserved, limit: limit, periodEnd: periodEnd}, nil
}

// IsZero reports whether s is "no snapshot".
func (s QuotaSnapshot) IsZero() bool { return s == QuotaSnapshot{} }

// Remaining is what can still be reserved.
func (s QuotaSnapshot) Remaining() QuotaAmount { return s.remaining }

// Reserved is held by requests that are still running.
func (s QuotaSnapshot) Reserved() QuotaAmount { return s.reserved }

// Limit is the allowance of the period.
func (s QuotaSnapshot) Limit() QuotaAmount { return s.limit }

// PeriodEnd is when the allowance renews; false when it does not.
func (s QuotaSnapshot) PeriodEnd() (time.Time, bool) { return s.periodEnd, !s.periodEnd.IsZero() }

// QuotaOutcome is the result of the last read of the remaining quota.
type QuotaOutcome int

// Outcomes of a read. The zero value is "not read yet".
const (
	QuotaUnchecked     QuotaOutcome = iota // not read since the start, or the key or the Hub address changed since
	QuotaCurrent                           // the last read succeeded
	QuotaNotConfigured                     // no usable Hub address
	QuotaNoKey                             // no Hub API key is stored
	QuotaKeyRejected                       // the Hub refused the key (invalid or revoked), or the stored key cannot be sent
	QuotaUnreachable                       // the Hub was not reached or did not answer in time
	QuotaFailed                            // any other failure
)

// QuotaReading is what the page knows about the quota: the outcome of the last read and the last snapshot that may
// still be shown, with its time. It is kept in memory only; the zero value is QuotaUnchecked without a snapshot.
type QuotaReading struct {
	outcome   QuotaOutcome
	snapshot  QuotaSnapshot
	fetchedAt time.Time // of snapshot
	checkedAt time.Time // of the last read
}

// Read records a successful read at at. A zero snapshot is refused and changes nothing.
func (r QuotaReading) Read(s QuotaSnapshot, at time.Time) (QuotaReading, error) {
	if s.IsZero() {
		return r, fmt.Errorf("%w: empty snapshot", ErrInvalidQuota)
	}
	return QuotaReading{outcome: QuotaCurrent, snapshot: s, fetchedAt: at, checkedAt: at}, nil
}

// Failed records a failed read at at. When the Hub was not reached or failed otherwise, the last snapshot stays as a
// stale value with its time. When the Hub is not configured, there is no key or the key was refused, the snapshot is
// dropped: it belongs to an account that is no longer the configured one, and a revoked key has no quota to show.
// An outcome that is not a failure counts as QuotaFailed.
func (r QuotaReading) Failed(o QuotaOutcome, at time.Time) QuotaReading {
	switch o {
	case QuotaNotConfigured, QuotaNoKey, QuotaKeyRejected:
		return QuotaReading{outcome: o, checkedAt: at}
	case QuotaUnreachable, QuotaFailed:
	default:
		o = QuotaFailed
	}
	r.outcome, r.checkedAt = o, at
	return r
}

// Outcome is the result of the last read.
func (r QuotaReading) Outcome() QuotaOutcome { return r.outcome }

// Snapshot is the last snapshot that may be shown; false when there is none.
func (r QuotaReading) Snapshot() (QuotaSnapshot, bool) { return r.snapshot, !r.snapshot.IsZero() }

// FetchedAt is when the snapshot was read; zero without one.
func (r QuotaReading) FetchedAt() time.Time { return r.fetchedAt }

// CheckedAt is when the last read ran; zero before the first.
func (r QuotaReading) CheckedAt() time.Time { return r.checkedAt }

// Stale reports a snapshot from an earlier read: the last read failed.
func (r QuotaReading) Stale() bool { return !r.snapshot.IsZero() && r.outcome != QuotaCurrent }
