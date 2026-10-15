package account

import (
	"fmt"
	"time"

	"paratech/hub/internal/domain/shared"
)

// KeyRecord is the stored form of an idempotency key of one tester: its fingerprint, when the fingerprint was
// recorded (the start of its retention), how many attempts it had, the reservation of the latest one and, when that
// attempt COMPLETED through the gateway, its stored Result. Attempt numbers keep counting when a stale record is
// renewed, so (key, attempt) stays unique among all reservations.
type KeyRecord struct {
	Key         IdempotencyKey
	Fingerprint Fingerprint
	CreatedAt   time.Time
	Attempts    int
	Latest      shared.ReservationID
	Result      Result
}

// stale reports whether the record has outlived the retention at now; the same text is then a new key and a stored
// result is never returned again (Policy.ResultCutoff).
func (k KeyRecord) stale(now time.Time, retention time.Duration) bool {
	return !now.Before(k.CreatedAt.Add(retention))
}

func validateKey(k KeyRecord) error {
	bad := func(why string) error { return fmt.Errorf("%w: idempotency key record: %s", ErrInvalidRecord, why) }
	if _, err := ParseIdempotencyKey(string(k.Key)); err != nil {
		return bad("invalid key")
	}
	if _, err := ParseOperation(string(k.Fingerprint.Operation)); err != nil {
		return bad("invalid operation")
	}
	if _, err := ParseModelAlias(string(k.Fingerprint.Alias)); err != nil {
		return bad("invalid model alias")
	}
	if h := k.Fingerprint.RequestHash; h != "" {
		if _, err := ParseRequestHash(string(h)); err != nil {
			return bad("invalid request hash")
		}
	}
	switch {
	case k.Fingerprint.Amount <= 0:
		return bad("amount must be positive")
	case k.CreatedAt.IsZero():
		return bad("creation time is required")
	case k.Attempts < 1:
		return bad("attempts must be positive")
	case k.Latest.Validate() != nil:
		return bad("invalid latest reservation id")
	case k.Result.validate() != nil:
		return bad("invalid result")
	case k.Result.Stored && k.Fingerprint.RequestHash == "":
		return bad("a stored result needs a request hash")
	}
	return nil
}

// validateResult checks that a stored result belongs to a latest attempt that was settled in full with its usage.
func validateResult(k KeyRecord, latest ReservationRecord) error {
	if !k.Result.Stored {
		return nil
	}
	if latest.State != StateSettled || latest.Partial || latest.Actual != k.Result.Total() {
		return fmt.Errorf("%w: idempotency key record: a stored result needs its attempt settled in full with the same usage", ErrInvalidRecord)
	}
	return nil
}
