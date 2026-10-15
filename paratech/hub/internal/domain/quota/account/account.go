// Package account is the quota aggregate of one tester (PRD section 9.2, AC-11): the used and held totals, the
// reservations (entities of the aggregate) and the idempotency keys that address them, with the stored result of a
// completed gateway attempt (Start, Complete). Every change of the totals produces a ledger Movement. The limit comes
// from the Policy, not from storage.
//
// Totals: used is what was charged, held what RESERVED reservations hold. remaining = max(0, limit - used - held).
// used + held never overflows int64: a hold is at most the remaining quota, and a charge above the hold at most
// what the account can bear.
package account

import (
	"fmt"
	"math"
	"time"

	"paratech/hub/internal/domain/shared"
)

// Account is the aggregate root. Build it with New or Restore (ARCH-14). It may be loaded partially (see Scope):
// it then knows the totals exactly but only the reservations and keys a command needs.
type Account struct {
	owner  shared.APIKeyID
	policy Policy
	used   int64
	held   int64

	reservations map[shared.ReservationID]*reservation
	order        []shared.ReservationID // load order, then creation order; keeps movements deterministic
	keys         map[IdempotencyKey]*KeyRecord

	created   []shared.ReservationID
	updated   []shared.ReservationID
	isUpdated map[shared.ReservationID]bool
	isCreated map[shared.ReservationID]bool
	keyWrites []IdempotencyKey
	movements []Movement
}

// Snapshot is the quota of the account at one moment.
type Snapshot struct {
	Unit      Unit
	Limit     int64
	Remaining int64
	Reserved  int64
}

// Changes is what a command changed, for the repository to persist in the same transaction: the new totals, the
// new and the changed reservations, the new or updated key records and the ledger movements, all in order.
type Changes struct {
	Used      int64
	Held      int64
	Created   []ReservationRecord
	Updated   []ReservationRecord
	Keys      []KeyRecord
	Movements []Movement
}

// New returns the empty account of a tester that has none yet.
func New(owner shared.APIKeyID, p Policy) (*Account, error) {
	return Restore(owner, p, 0, 0, nil, nil)
}

// Restore rebuilds a stored account with the loaded part of its reservations and keys, without side effects. Every
// key record must come with the reservation of its latest attempt.
func Restore(owner shared.APIKeyID, p Policy, used, held int64, rs []ReservationRecord, keys []KeyRecord) (*Account, error) {
	if err := owner.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidRecord, err)
	}
	if !p.valid() {
		return nil, fmt.Errorf("%w: policy is not set", ErrInvalidPolicy)
	}
	if used < 0 || held < 0 || used > math.MaxInt64-held {
		return nil, fmt.Errorf("%w: totals must be non-negative and their sum must fit int64", ErrInvalidRecord)
	}

	a := &Account{
		owner: owner, policy: p, used: used, held: held,
		reservations: make(map[shared.ReservationID]*reservation, len(rs)),
		keys:         make(map[IdempotencyKey]*KeyRecord, len(keys)),
		isUpdated:    map[shared.ReservationID]bool{},
		isCreated:    map[shared.ReservationID]bool{},
	}
	var holding int64
	for _, rec := range rs {
		r, err := restoreReservation(rec)
		if err != nil {
			return nil, err
		}
		if _, dup := a.reservations[rec.ID]; dup {
			return nil, fmt.Errorf("%w: reservation loaded twice", ErrInvalidRecord)
		}
		if r.rec.State == StateReserved {
			if r.rec.Amount > held-holding {
				return nil, fmt.Errorf("%w: reservations hold more than the account total", ErrInvalidRecord)
			}
			holding += r.rec.Amount
		}
		a.reservations[rec.ID] = r
		a.order = append(a.order, rec.ID)
	}
	for _, k := range keys {
		if err := validateKey(k); err != nil {
			return nil, err
		}
		if _, dup := a.keys[k.Key]; dup {
			return nil, fmt.Errorf("%w: key record loaded twice", ErrInvalidRecord)
		}
		latest, ok := a.reservations[k.Latest]
		if !ok || latest.rec.Key != k.Key || latest.rec.Attempt != k.Attempts || latest.rec.Amount != k.Fingerprint.Amount {
			return nil, fmt.Errorf("%w: key record does not match its latest reservation", ErrInvalidRecord)
		}
		if err := validateResult(k, latest.rec); err != nil {
			return nil, err
		}
		a.keys[k.Key] = &k // a per-iteration copy (Go 1.22 loop semantics)
	}
	return a, nil
}

// Owner returns the API key the account belongs to.
func (a *Account) Owner() shared.APIKeyID { return a.owner }

// Used returns the charged total.
func (a *Account) Used() int64 { return a.used }

// Held returns the total held by RESERVED reservations.
func (a *Account) Held() int64 { return a.held }

// Snapshot returns the quota at now. It changes nothing: overdue holds that are loaded count as already released, so
// a read shows what the next command will see.
func (a *Account) Snapshot(now time.Time) Snapshot {
	held := a.held
	for _, id := range a.order {
		if r := a.reservations[id]; r.overdue(now) {
			held -= r.rec.Amount
		}
	}
	return Snapshot{Unit: a.policy.unit, Limit: a.policy.limit, Remaining: a.available(a.used + held), Reserved: held}
}

// available is what can still be reserved or charged when committed (used plus holds) is taken.
func (a *Account) available(committed int64) int64 {
	if committed >= a.policy.limit {
		return 0
	}
	return a.policy.limit - committed
}

// Reserve holds req's amount for its idempotency key (quota.proto, key life cycle; ADR-0005(d)), deciding by the
// latest attempt of the key: an unexpired RESERVED one or a complete SETTLED one is returned as it is (replayed is
// true); otherwise a new reservation with id is opened. A different fingerprint is ErrIdempotencyConflict in every
// state; an amount above the remaining quota is ErrQuotaExceeded and changes nothing about the key. A key record
// older than the retention is a new key, unless its latest attempt is still RESERVED.
func (a *Account) Reserve(req ReserveRequest, id shared.ReservationID, now time.Time) (rec ReservationRecord, replayed bool, err error) {
	if err := a.checkCommand(now); err != nil {
		return ReservationRecord{}, false, err
	}
	if req.unit != a.policy.unit || req.key == "" {
		return ReservationRecord{}, false, fmt.Errorf("%w: reserve request is not valid for this account", ErrInvalidArgument)
	}
	if err := id.Validate(); err != nil {
		return ReservationRecord{}, false, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if err := a.expire(now); err != nil {
		return ReservationRecord{}, false, err
	}

	k := a.keys[req.key]
	// A key also stays live past its retention while its latest attempt still holds quota (expired ones were released
	// above); otherwise a second hold could be opened for one key. Restore guarantees the latest attempt is loaded.
	live := k != nil && (!k.stale(now, a.policy.retention) || a.reservations[k.Latest].rec.State == StateReserved)
	if live {
		if k.Fingerprint != req.fingerprint {
			return ReservationRecord{}, false, ErrIdempotencyConflict
		}
		latest := a.reservations[k.Latest].rec
		if latest.State == StateReserved || (latest.State == StateSettled && !latest.Partial) {
			return latest, true, nil
		}
	}

	amount := req.fingerprint.Amount
	if amount > a.available(a.used+a.held) {
		return ReservationRecord{}, false, ErrQuotaExceeded
	}
	if _, dup := a.reservations[id]; dup {
		return ReservationRecord{}, false, fmt.Errorf("%w: reservation id is already used", ErrInvalidArgument)
	}

	attempt := 1
	if k != nil {
		attempt = k.Attempts + 1 // also across a renewal, so (key, attempt) stays unique
	}
	r := &reservation{rec: ReservationRecord{
		ID: id, Key: req.key, Attempt: attempt, Amount: amount, State: StateReserved,
		CreatedAt: now, ExpiresAt: now.Add(a.policy.ttl),
	}}
	a.reservations[id] = r
	a.order = append(a.order, id)
	a.created = append(a.created, id)
	a.isCreated[id] = true
	a.held += amount
	a.movements = append(a.movements, Movement{Kind: MovementReserve, Reservation: id, HeldDelta: amount, At: now})

	if live {
		k.Attempts, k.Latest, k.Result = attempt, id, Result{}
	} else {
		a.keys[req.key] = &KeyRecord{Key: req.key, Fingerprint: req.fingerprint, CreatedAt: now, Attempts: attempt, Latest: id}
	}
	a.keyWrites = append(a.keyWrites, req.key)
	return r.rec, false, nil
}

// Settle charges the actual usage of reservation id and returns the rest of its hold. The charge is the actual
// usage, bounded above the hold by what the account can bear (never below a zero balance). A reservation released
// by expiry is settled once the same way, with no hold to return. A repeated Settle with the same usage returns the
// original outcome; another usage is ErrIdempotencyConflict. Settling a reservation released by Release or after a
// failed call is ErrReleased.
func (a *Account) Settle(id shared.ReservationID, u Usage, now time.Time) (ReservationRecord, error) {
	if err := a.checkCommand(now); err != nil {
		return ReservationRecord{}, err
	}
	if u.unit != a.policy.unit {
		return ReservationRecord{}, fmt.Errorf("%w: usage unit does not match the account", ErrInvalidArgument)
	}
	if err := a.expire(now); err != nil {
		return ReservationRecord{}, err
	}
	r, ok := a.reservations[id]
	if !ok {
		return ReservationRecord{}, ErrReservationNotFound
	}

	switch r.phase() {
	case phaseSettled:
		if r.rec.Actual != u.actual || r.rec.Partial != u.partial {
			return r.rec, ErrIdempotencyConflict
		}
		return r.rec, nil
	case phaseReleasedExplicit, phaseReleasedFailed:
		return r.rec, ErrReleased
	}

	var hold int64
	if r.rec.State == StateReserved {
		hold = r.rec.Amount
	}
	// What the account can bear without this reservation's hold; the hold itself was granted, so it is always bearable.
	limit := max(hold, a.available(a.used+a.held-hold))
	charge := min(u.actual, limit)
	if err := r.moveTo(phaseSettled, now); err != nil {
		return r.rec, err
	}
	r.rec.Charged, r.rec.Actual, r.rec.Partial = charge, u.actual, u.partial
	a.held -= hold
	a.used += charge
	a.markUpdated(id)
	a.movements = append(a.movements, Movement{Kind: MovementSettle, Reservation: id, HeldDelta: -hold, UsedDelta: charge, At: now})
	return r.rec, nil
}

// Release returns the whole hold of reservation id without charging (reason explicit for Release, failed for a call
// that failed before the provider answered). Releasing a RELEASED reservation is a no-op; a SETTLED one stays as it
// is (the charge stands). Both report the reservation unchanged.
func (a *Account) Release(id shared.ReservationID, reason ReleaseReason, now time.Time) (ReservationRecord, error) {
	if err := a.checkCommand(now); err != nil {
		return ReservationRecord{}, err
	}
	to := phaseOf(StateReleased, reason)
	if to != phaseReleasedExplicit && to != phaseReleasedFailed {
		return ReservationRecord{}, fmt.Errorf("%w: release reason must be explicit or failed", ErrInvalidArgument)
	}
	if err := a.expire(now); err != nil {
		return ReservationRecord{}, err
	}
	r, ok := a.reservations[id]
	if !ok {
		return ReservationRecord{}, ErrReservationNotFound
	}
	if r.rec.State != StateReserved {
		return r.rec, nil
	}
	if err := r.moveTo(to, now); err != nil {
		return r.rec, err
	}
	a.held -= r.rec.Amount
	a.markUpdated(id)
	a.movements = append(a.movements, Movement{Kind: MovementRelease, Reservation: id, HeldDelta: -r.rec.Amount, At: now})
	return r.rec, nil
}

// Start begins a gateway call with req's key (gateway.proto, key life cycle), deciding by the latest attempt like
// Reserve: a new attempt opens reservation id and returns it with no result; a COMPLETED latest attempt returns its
// reservation and its stored result (Result.Stored), so nothing is held or charged again; a RUNNING one (RESERVED and
// not expired) is ErrAttemptRunning. req must carry a request hash, so a different request is ErrIdempotencyConflict
// before anything is replayed. The other errors are those of Reserve. A COMPLETED attempt without its result (not
// written by the gateway) is ErrResultMissing.
func (a *Account) Start(req ReserveRequest, id shared.ReservationID, now time.Time) (ReservationRecord, Result, error) {
	if req.fingerprint.RequestHash == "" {
		return ReservationRecord{}, Result{}, fmt.Errorf("%w: a gateway call needs a request hash", ErrInvalidArgument)
	}
	rec, replayed, err := a.Reserve(req, id, now)
	if err != nil || !replayed {
		return rec, Result{}, err
	}
	if rec.State == StateReserved {
		return rec, Result{}, ErrAttemptRunning
	}
	res := a.keys[req.key].Result
	if !res.Stored {
		return rec, Result{}, ErrResultMissing
	}
	return rec, res, nil
}

// Complete settles reservation id in full with u and keeps res as the result of its attempt. The result is stored
// only while the attempt is the latest of its key and the key is within its retention; a late attempt (a newer one
// owns the key, quota.proto) is charged but its result is discarded. The key record must be loaded (Scope.Key and
// Scope.Reservation). u must not be partial and must equal the result's usage. A repeat keeps the first result; the
// errors are those of Settle.
func (a *Account) Complete(id shared.ReservationID, u Usage, res Result, now time.Time) (ReservationRecord, error) {
	if u.partial || res.validate() != nil || !res.Stored || res.Total() != u.actual {
		return ReservationRecord{}, fmt.Errorf("%w: a complete settlement needs a result with the same usage", ErrInvalidArgument)
	}
	if r, ok := a.reservations[id]; ok && a.keys[r.rec.Key] == nil {
		return ReservationRecord{}, fmt.Errorf("%w: the key record of the reservation is not loaded", ErrInvalidArgument)
	}
	rec, err := a.Settle(id, u, now)
	if err != nil {
		return rec, err
	}
	k := a.keys[rec.Key]
	if k.Latest != id || k.stale(now, a.policy.retention) || k.Result.Stored {
		return rec, nil
	}
	k.Result = res
	a.keyWrites = append(a.keyWrites, k.Key)
	return rec, nil
}

// Changes returns what the commands since Restore changed, for the repository.
func (a *Account) Changes() Changes {
	ch := Changes{Used: a.used, Held: a.held, Movements: append([]Movement(nil), a.movements...)}
	for _, id := range a.created {
		ch.Created = append(ch.Created, a.reservations[id].rec)
	}
	for _, id := range a.updated {
		ch.Updated = append(ch.Updated, a.reservations[id].rec)
	}
	written := map[IdempotencyKey]bool{}
	for _, k := range a.keyWrites {
		if !written[k] {
			written[k] = true
			ch.Keys = append(ch.Keys, *a.keys[k])
		}
	}
	return ch
}

// expire releases every loaded RESERVED reservation that reached its expiry at now (lazy expiry, ADR-0008).
func (a *Account) expire(now time.Time) error {
	for _, id := range a.order {
		r := a.reservations[id]
		if !r.overdue(now) {
			continue
		}
		if err := r.moveTo(phaseReleasedExpired, now); err != nil {
			return err
		}
		a.held -= r.rec.Amount
		a.markUpdated(id)
		a.movements = append(a.movements, Movement{Kind: MovementExpire, Reservation: id, HeldDelta: -r.rec.Amount, At: now})
	}
	return nil
}

func (a *Account) markUpdated(id shared.ReservationID) {
	if a.isCreated[id] || a.isUpdated[id] {
		return
	}
	a.isUpdated[id] = true
	a.updated = append(a.updated, id)
}

func (a *Account) checkCommand(now time.Time) error {
	if now.IsZero() {
		return fmt.Errorf("%w: the time of the command is required", ErrInvalidArgument)
	}
	return nil
}
