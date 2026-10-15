package account

import (
	"fmt"
	"time"

	"paratech/hub/internal/domain/shared"
)

// KeyRecord is the stored form of an idempotency key of one tester: its fingerprint, when the fingerprint was
// recorded (the start of its retention), how many attempts it had and the reservation of the latest one. Attempt
// numbers keep counting when a stale record is renewed, so (key, attempt) stays unique among all reservations.
type KeyRecord struct {
	Key         IdempotencyKey
	Fingerprint Fingerprint
	CreatedAt   time.Time
	Attempts    int
	Latest      shared.ReservationID
}

// stale reports whether the record has outlived the retention at now; the same text is then a new key.
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
	switch {
	case k.Fingerprint.Amount <= 0:
		return bad("amount must be positive")
	case k.CreatedAt.IsZero():
		return bad("creation time is required")
	case k.Attempts < 1:
		return bad("attempts must be positive")
	case k.Latest.Validate() != nil:
		return bad("invalid latest reservation id")
	}
	return nil
}
