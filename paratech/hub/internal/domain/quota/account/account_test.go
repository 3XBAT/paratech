package account

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"paratech/hub/internal/domain/shared"
)

const (
	owner     shared.APIKeyID = "0123456789abcdef"
	ttl                       = 10 * time.Minute
	retention                 = 24 * time.Hour
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func rid(n int) shared.ReservationID { return shared.ReservationID(fmt.Sprintf("%032x", n)) }

func policy(t *testing.T, limit int64) Policy {
	t.Helper()
	p, err := NewPolicy("tokens", limit, ttl, retention)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newAccount(t *testing.T, limit int64) *Account {
	t.Helper()
	a, err := New(owner, policy(t, limit))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func request(t *testing.T, key string, amount int64) ReserveRequest {
	t.Helper()
	r, err := NewReserveRequest(key, "summary", "fast", "tokens", amount)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func usage(t *testing.T, actual int64, partial bool) Usage {
	t.Helper()
	u, err := NewUsage("tokens", actual, partial)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func reserve(t *testing.T, a *Account, key string, amount int64, id shared.ReservationID, now time.Time) ReservationRecord {
	t.Helper()
	rec, replayed, err := a.Reserve(request(t, key, amount), id, now)
	if err != nil || replayed {
		t.Fatalf("Reserve(%s, %d) = %+v, replayed %v, %v", key, amount, rec, replayed, err)
	}
	return rec
}

// reload persists the changes the way a repository does and restores the account with the given scope, so that
// tests also run through Restore.
func reload(t *testing.T, a *Account, now time.Time, key IdempotencyKey, id shared.ReservationID) *Account {
	t.Helper()
	all := map[shared.ReservationID]ReservationRecord{}
	for _, r := range a.reservations {
		all[r.rec.ID] = r.rec
	}
	var rs []ReservationRecord
	seen := map[shared.ReservationID]bool{}
	add := func(r ReservationRecord) {
		if !seen[r.ID] {
			seen[r.ID] = true
			rs = append(rs, r)
		}
	}
	for _, rid := range a.order {
		if r := all[rid]; r.State == StateReserved && !now.Before(r.ExpiresAt) {
			add(r)
		}
	}
	var keys []KeyRecord
	if k, ok := a.keys[key]; ok {
		keys = append(keys, *k)
		add(all[k.Latest])
	}
	if r, ok := all[id]; ok {
		add(r)
	}
	b, err := Restore(a.owner, a.policy, a.used, a.held, rs, keys)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return b
}

func assertTotals(t *testing.T, a *Account, used, held int64) {
	t.Helper()
	if a.Used() != used || a.Held() != held {
		t.Fatalf("used %d held %d, want used %d held %d", a.Used(), a.Held(), used, held)
	}
	// The ledger of this account instance moves the totals exactly: it starts from zero in these tests.
	var h, u int64
	for _, m := range a.movements {
		h += m.HeldDelta
		u += m.UsedDelta
	}
	if h != held || u != used {
		t.Fatalf("ledger sums held %d used %d, totals held %d used %d", h, u, held, used)
	}
}

func TestReserveNewKey(t *testing.T) {
	a := newAccount(t, 1000)
	rec := reserve(t, a, "k", 300, rid(1), t0)
	if rec.ID != rid(1) || rec.State != StateReserved || rec.Amount != 300 || rec.Attempt != 1 ||
		!rec.CreatedAt.Equal(t0) || !rec.ExpiresAt.Equal(t0.Add(ttl)) || rec.Charged != 0 {
		t.Fatalf("reservation = %+v", rec)
	}
	assertTotals(t, a, 0, 300)
	if s := a.Snapshot(t0); s != (Snapshot{Unit: UnitTokens, Limit: 1000, Remaining: 700, Reserved: 300}) {
		t.Fatalf("snapshot = %+v", s)
	}
	ch := a.Changes()
	if len(ch.Created) != 1 || len(ch.Updated) != 0 || len(ch.Keys) != 1 || len(ch.Movements) != 1 || ch.Held != 300 || ch.Used != 0 {
		t.Fatalf("changes = %+v", ch)
	}
	k := ch.Keys[0]
	if k.Key != "k" || k.Attempts != 1 || k.Latest != rid(1) || !k.CreatedAt.Equal(t0) ||
		k.Fingerprint != (Fingerprint{Operation: OperationSummary, Alias: "fast", Amount: 300}) {
		t.Fatalf("key record = %+v", k)
	}
	if m := ch.Movements[0]; m != (Movement{Kind: MovementReserve, Reservation: rid(1), HeldDelta: 300, At: t0}) {
		t.Fatalf("movement = %+v", m)
	}
}

// TestReserveDecisionTable covers the key life cycle of quota.proto by the state of the latest attempt.
func TestReserveDecisionTable(t *testing.T) {
	type step func(t *testing.T, a *Account) time.Time // brings the latest attempt into a state, returns "now"
	cases := []struct {
		name       string
		prepare    step
		wantReplay bool
	}{
		{"reserved, not expired", func(*testing.T, *Account) time.Time { return t0.Add(ttl - time.Second) }, true},
		{"reserved, expired", func(*testing.T, *Account) time.Time { return t0.Add(ttl) }, false},
		{"settled complete", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Settle(rid(1), usage(t, 50, false), t0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			return t0.Add(2 * time.Minute)
		}, true},
		{"settled partial", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Settle(rid(1), usage(t, 50, true), t0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			return t0.Add(2 * time.Minute)
		}, false},
		{"released explicitly", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Release(rid(1), ReasonExplicit, t0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			return t0.Add(2 * time.Minute)
		}, false},
		{"released after failure", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Release(rid(1), ReasonFailed, t0.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			return t0.Add(2 * time.Minute)
		}, false},
		{"released by expiry earlier", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Release(rid(1), ReasonExplicit, t0.Add(ttl)); err != nil { // expiry runs first; Release is a no-op
				t.Fatal(err)
			}
			if a.reservations[rid(1)].phase() != phaseReleasedExpired {
				t.Fatalf("phase = %q, want released/expired", a.reservations[rid(1)].phase())
			}
			return t0.Add(ttl + time.Minute)
		}, false},
		{"settled late after expiry", func(t *testing.T, a *Account) time.Time {
			if _, err := a.Settle(rid(1), usage(t, 70, false), t0.Add(ttl+time.Second)); err != nil {
				t.Fatal(err)
			}
			return t0.Add(ttl + time.Minute)
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newAccount(t, 1000)
			first := reserve(t, a, "k", 100, rid(1), t0)
			now := tc.prepare(t, a)
			a = reload(t, a, now, "k", "")
			before := a.Snapshot(now)

			// A different fingerprint is a conflict in every state and changes nothing.
			for name, r := range map[string]ReserveRequest{
				"amount":    request(t, "k", 101),
				"alias":     mustRequest(t, "k", "summary", "slow", 100),
				"operation": mustRequest(t, "k", "homework_draft", "fast", 100),
			} {
				if _, _, err := a.Reserve(r, rid(9), now); !errors.Is(err, ErrIdempotencyConflict) {
					t.Errorf("different %s: err = %v, want ErrIdempotencyConflict", name, err)
				}
			}

			got, replayed, err := a.Reserve(request(t, "k", 100), rid(2), now)
			if err != nil {
				t.Fatal(err)
			}
			if replayed != tc.wantReplay {
				t.Fatalf("replayed = %v, want %v", replayed, tc.wantReplay)
			}
			ch := a.Changes()
			if tc.wantReplay {
				if got.ID != first.ID || len(ch.Created) != 0 || len(ch.Keys) != 0 || a.Snapshot(now) != before {
					t.Fatalf("replay changed something: %+v, changes %+v", got, ch)
				}
				return
			}
			if got.ID != rid(2) || got.Attempt != 2 || got.State != StateReserved {
				t.Fatalf("new attempt = %+v", got)
			}
			if len(ch.Created) != 1 || len(ch.Keys) != 1 || ch.Keys[0].Attempts != 2 || ch.Keys[0].Latest != rid(2) || !ch.Keys[0].CreatedAt.Equal(t0) {
				t.Fatalf("changes = %+v", ch)
			}
			if after := a.Snapshot(now); after.Reserved != 100 || after.Remaining != before.Remaining-100 {
				t.Fatalf("snapshot %+v after %+v", after, before)
			}
		})
	}
}

func mustRequest(t *testing.T, key, op, alias string, amount int64) ReserveRequest {
	t.Helper()
	r, err := NewReserveRequest(key, op, alias, "tokens", amount)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReserveExpiredLatestIsReleasedInTheLedger(t *testing.T) {
	a := newAccount(t, 1000)
	reserve(t, a, "k", 100, rid(1), t0)
	now := t0.Add(ttl)
	a = reload(t, a, now, "k", "")
	if _, _, err := a.Reserve(request(t, "k", 100), rid(2), now); err != nil {
		t.Fatal(err)
	}
	ch := a.Changes()
	want := []Movement{
		{Kind: MovementExpire, Reservation: rid(1), HeldDelta: -100, At: now},
		{Kind: MovementReserve, Reservation: rid(2), HeldDelta: 100, At: now},
	}
	if len(ch.Movements) != 2 || ch.Movements[0] != want[0] || ch.Movements[1] != want[1] {
		t.Fatalf("movements = %+v, want %+v", ch.Movements, want)
	}
	if len(ch.Updated) != 1 || ch.Updated[0].Reason != ReasonExpired || !ch.Updated[0].ClosedAt.Equal(now) || ch.Held != 100 {
		t.Fatalf("changes = %+v", ch)
	}
}

func TestReserveQuotaExceeded(t *testing.T) {
	a := newAccount(t, 1000)
	reserve(t, a, "a", 600, rid(1), t0)
	before := a.Snapshot(t0)
	movements := len(a.movements)

	for _, amount := range []int64{401, 1000, 1001, math.MaxInt64} {
		if _, _, err := a.Reserve(request(t, "b", amount), rid(2), t0); !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("Reserve(%d) = %v, want ErrQuotaExceeded", amount, err)
		}
	}
	if a.Snapshot(t0) != before || len(a.movements) != movements || a.keys["b"] != nil || a.reservations[rid(2)] != nil {
		t.Fatal("a refused Reserve changed the account")
	}

	// Exactly the remaining amount fits; then nothing does.
	reserve(t, a, "b", 400, rid(2), t0)
	if s := a.Snapshot(t0); s.Remaining != 0 || s.Reserved != 1000 {
		t.Fatalf("snapshot = %+v", s)
	}
	if _, _, err := a.Reserve(request(t, "c", 1), rid(3), t0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
}

// TestReserveExceededKeepsTheKey checks that QUOTA_EXCEEDED on a later attempt records nothing for the key.
func TestReserveExceededKeepsTheKey(t *testing.T) {
	a := newAccount(t, 100)
	reserve(t, a, "k", 100, rid(1), t0)
	if _, err := a.Release(rid(1), ReasonFailed, t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	reserve(t, a, "other", 50, rid(2), t0.Add(2*time.Second))
	now := t0.Add(3 * time.Second)
	a = reload(t, a, now, "k", "")
	if _, _, err := a.Reserve(request(t, "k", 100), rid(3), now); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	if ch := a.Changes(); len(ch.Keys) != 0 || len(ch.Created) != 0 || len(ch.Movements) != 0 {
		t.Fatalf("changes after QUOTA_EXCEEDED = %+v", ch)
	}
	if k := a.keys["k"]; k.Attempts != 1 || k.Latest != rid(1) {
		t.Fatalf("key record changed: %+v", k)
	}
}

func TestReserveRetention(t *testing.T) {
	a := newAccount(t, 1000)
	reserve(t, a, "k", 100, rid(1), t0)
	if _, err := a.Settle(rid(1), usage(t, 40, false), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Just before the retention ends the key replays, and another fingerprint conflicts.
	now := t0.Add(retention - time.Second)
	a = reload(t, a, now, "k", "")
	if got, replayed, err := a.Reserve(request(t, "k", 100), rid(2), now); err != nil || !replayed || got.ID != rid(1) {
		t.Fatalf("before retention: %+v %v %v", got, replayed, err)
	}
	if _, _, err := a.Reserve(request(t, "k", 200), rid(2), now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("before retention: err = %v, want conflict", err)
	}

	// From the end of the retention the same text is a new key with a fresh fingerprint; the old reservation stays.
	now = t0.Add(retention)
	a = reload(t, a, now, "k", "")
	got, replayed, err := a.Reserve(request(t, "k", 200), rid(2), now)
	if err != nil || replayed || got.ID != rid(2) || got.Attempt != 2 {
		t.Fatalf("after retention: %+v %v %v", got, replayed, err)
	}
	k := a.Changes().Keys[0]
	if !k.CreatedAt.Equal(now) || k.Fingerprint.Amount != 200 || k.Attempts != 2 || k.Latest != rid(2) {
		t.Fatalf("renewed key record = %+v", k)
	}
	if old := a.reservations[rid(1)].rec; old.State != StateSettled || old.Charged != 40 {
		t.Fatalf("old reservation changed: %+v", old)
	}
}

// TestReserveRetentionKeepsReservedLatest checks that a key stays live past its retention while its latest attempt
// is RESERVED and unexpired: the same fingerprint replays it, another one conflicts, and no second hold is opened.
func TestReserveRetentionKeepsReservedLatest(t *testing.T) {
	a := newAccount(t, 1000)
	reserve(t, a, "k", 100, rid(1), t0)
	if _, err := a.Release(rid(1), ReasonFailed, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	opened := t0.Add(retention - ttl/2)
	a = reload(t, a, opened, "k", "")
	second := reserve(t, a, "k", 100, rid(2), opened)

	now := t0.Add(retention) // the key record is past its retention, attempt 2 holds until opened+ttl
	a = reload(t, a, now, "k", "")
	before := a.Snapshot(now)
	if _, _, err := a.Reserve(request(t, "k", 200), rid(3), now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different fingerprint: err = %v, want ErrIdempotencyConflict", err)
	}
	got, replayed, err := a.Reserve(request(t, "k", 100), rid(3), now)
	if err != nil || !replayed || got != second {
		t.Fatalf("same fingerprint: %+v replayed %v, %v; want replay of %+v", got, replayed, err, second)
	}
	if ch := a.Changes(); len(ch.Created) != 0 || len(ch.Keys) != 0 || len(ch.Movements) != 0 || a.Snapshot(now) != before {
		t.Fatalf("a replay past retention changed something: %+v", ch)
	}

	// Once attempt 2 expires the stale key is a new key again.
	now = opened.Add(ttl)
	a = reload(t, a, now, "k", "")
	got, replayed, err = a.Reserve(request(t, "k", 200), rid(3), now)
	if err != nil || replayed || got.ID != rid(3) || got.Attempt != 3 {
		t.Fatalf("after attempt 2 expired: %+v %v %v", got, replayed, err)
	}
	if k := a.Changes().Keys[0]; !k.CreatedAt.Equal(now) || k.Fingerprint.Amount != 200 {
		t.Fatalf("renewed key record = %+v", k)
	}
}

// TestReserveRetentionWithExceeded checks that a stale key record survives a QUOTA_EXCEEDED renewal attempt.
func TestReserveRetentionWithExceeded(t *testing.T) {
	a := newAccount(t, 100)
	reserve(t, a, "k", 10, rid(1), t0)
	now := t0.Add(retention)
	a = reload(t, a, now, "k", "")
	if _, _, err := a.Reserve(request(t, "k", 500), rid(2), now); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	if ch := a.Changes(); len(ch.Keys) != 0 || len(ch.Created) != 0 {
		t.Fatalf("changes = %+v", ch)
	}
}

func TestReserveRejects(t *testing.T) {
	a := newAccount(t, 1000)
	reserve(t, a, "k", 10, rid(1), t0)
	for name, call := range map[string]func() error{
		"zero time":    func() error { _, _, err := a.Reserve(request(t, "x", 1), rid(5), time.Time{}); return err },
		"zero request": func() error { _, _, err := a.Reserve(ReserveRequest{}, rid(5), t0); return err },
		"invalid id":   func() error { _, _, err := a.Reserve(request(t, "x", 1), "nope", t0); return err },
		"id is used":   func() error { _, _, err := a.Reserve(request(t, "x", 1), rid(1), t0); return err },
		"foreign unit": func() error {
			r := request(t, "x", 1)
			r.unit = "credits"
			_, _, err := a.Reserve(r, rid(5), t0)
			return err
		},
		"settle unit": func() error {
			u := usage(t, 1, false)
			u.unit = "credits"
			_, err := a.Settle(rid(1), u, t0)
			return err
		},
		"settle time":   func() error { _, err := a.Settle(rid(1), usage(t, 1, false), time.Time{}); return err },
		"release time":  func() error { _, err := a.Release(rid(1), ReasonExplicit, time.Time{}); return err },
		"release none":  func() error { _, err := a.Release(rid(1), ReasonNone, t0); return err },
		"release other": func() error { _, err := a.Release(rid(1), ReasonExpired, t0); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	if s := a.Snapshot(t0); s.Reserved != 10 || len(a.created) != 1 {
		t.Fatalf("a rejected command changed the account: %+v", s)
	}
}

func TestSettle(t *testing.T) {
	t.Run("less than reserved returns the unused part", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		now := t0.Add(time.Minute)
		a = reload(t, a, now, "", rid(1))
		got, err := a.Settle(rid(1), usage(t, 120, false), now)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != StateSettled || got.Charged != 120 || got.Actual != 120 || got.Partial || !got.ClosedAt.Equal(now) {
			t.Fatalf("settled = %+v", got)
		}
		if s := a.Snapshot(now); s.Remaining != 880 || s.Reserved != 0 {
			t.Fatalf("snapshot = %+v", s)
		}
		ch := a.Changes()
		if len(ch.Updated) != 1 || len(ch.Movements) != 1 ||
			ch.Movements[0] != (Movement{Kind: MovementSettle, Reservation: rid(1), HeldDelta: -300, UsedDelta: 120, At: now}) {
			t.Fatalf("changes = %+v", ch)
		}
	})

	t.Run("more than reserved is charged while the account can bear it", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		got, err := a.Settle(rid(1), usage(t, 900, false), t0)
		if err != nil || got.Charged != 900 || got.Actual != 900 {
			t.Fatalf("settled = %+v, %v", got, err)
		}
		assertTotals(t, a, 900, 0)
	})

	t.Run("more than the account can bear is clamped", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		reserve(t, a, "other", 500, rid(2), t0)
		got, err := a.Settle(rid(1), usage(t, math.MaxInt64, false), t0)
		if err != nil || got.Charged != 500 || got.Actual != math.MaxInt64 {
			t.Fatalf("settled = %+v, %v", got, err)
		}
		assertTotals(t, a, 500, 500)
		if s := a.Snapshot(t0); s.Remaining != 0 {
			t.Fatalf("snapshot = %+v", s)
		}
	})

	t.Run("the hold stays bearable after the limit was lowered", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		lowered, err := Restore(owner, policy(t, 100), a.used, a.held, []ReservationRecord{a.reservations[rid(1)].rec}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := lowered.Settle(rid(1), usage(t, 400, false), t0)
		if err != nil || got.Charged != 300 {
			t.Fatalf("settled = %+v, %v", got, err)
		}
		if s := lowered.Snapshot(t0); s.Remaining != 0 || s.Limit != 100 || lowered.Used() != 300 {
			t.Fatalf("snapshot = %+v used %d", s, lowered.Used())
		}
	})

	t.Run("zero usage", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		if got, err := a.Settle(rid(1), usage(t, 0, true), t0); err != nil || got.Charged != 0 || !got.Partial {
			t.Fatalf("settled = %+v, %v", got, err)
		}
		assertTotals(t, a, 0, 0)
	})

	t.Run("repeat returns the outcome, other usage conflicts", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		first, err := a.Settle(rid(1), usage(t, 120, true), t0)
		if err != nil {
			t.Fatal(err)
		}
		now := t0.Add(time.Minute)
		a = reload(t, a, now, "", rid(1))
		again, err := a.Settle(rid(1), usage(t, 120, true), now)
		if err != nil || again != first {
			t.Fatalf("repeat = %+v, %v; want %+v", again, err, first)
		}
		for _, u := range []Usage{usage(t, 121, true), usage(t, 120, false)} {
			if _, err := a.Settle(rid(1), u, now); !errors.Is(err, ErrIdempotencyConflict) {
				t.Errorf("Settle(%+v) = %v, want ErrIdempotencyConflict", u, err)
			}
		}
		if ch := a.Changes(); len(ch.Updated) != 0 || len(ch.Movements) != 0 {
			t.Fatalf("a repeated Settle changed something: %+v", ch)
		}
	})

	t.Run("released by Release or failure is refused", func(t *testing.T) {
		for _, reason := range []ReleaseReason{ReasonExplicit, ReasonFailed} {
			a := newAccount(t, 1000)
			reserve(t, a, "k", 300, rid(1), t0)
			if _, err := a.Release(rid(1), reason, t0); err != nil {
				t.Fatal(err)
			}
			got, err := a.Settle(rid(1), usage(t, 10, false), t0)
			if !errors.Is(err, ErrReleased) || got.State != StateReleased {
				t.Fatalf("%s: Settle = %+v, %v; want ErrReleased", reason, got, err)
			}
			assertTotals(t, a, 0, 0)
		}
	})

	t.Run("released by expiry is settled once", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		reserve(t, a, "other", 600, rid(2), t0.Add(ttl/2))
		now := t0.Add(ttl + time.Minute) // rid(1) is overdue, rid(2) is not
		a = reload(t, a, now, "", rid(1))
		if s := a.Snapshot(now); s.Reserved != 600 || s.Remaining != 400 {
			t.Fatalf("snapshot ignores the overdue hold: %+v", s)
		}
		got, err := a.Settle(rid(1), usage(t, 500, false), now)
		if err != nil || got.State != StateSettled || got.Charged != 400 || got.Actual != 500 {
			t.Fatalf("late Settle = %+v, %v; want 400 charged (all the account can bear)", got, err)
		}
		ch := a.Changes()
		want := []Movement{
			{Kind: MovementExpire, Reservation: rid(1), HeldDelta: -300, At: now},
			{Kind: MovementSettle, Reservation: rid(1), UsedDelta: 400, At: now},
		}
		if len(ch.Movements) != 2 || ch.Movements[0] != want[0] || ch.Movements[1] != want[1] || len(ch.Updated) != 1 {
			t.Fatalf("changes = %+v", ch)
		}
		if ch.Used != 400 || ch.Held != 600 {
			t.Fatalf("totals used %d held %d", ch.Used, ch.Held)
		}
		// Once settled it replays like any settled reservation.
		if again, err := a.Settle(rid(1), usage(t, 500, false), now); err != nil || again != got {
			t.Fatalf("repeat = %+v, %v", again, err)
		}
	})

	t.Run("unknown reservation", func(t *testing.T) {
		a := newAccount(t, 1000)
		if _, err := a.Settle(rid(7), usage(t, 1, false), t0); !errors.Is(err, ErrReservationNotFound) {
			t.Fatalf("err = %v, want ErrReservationNotFound", err)
		}
	})
}

func TestRelease(t *testing.T) {
	t.Run("returns the whole hold", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		now := t0.Add(time.Minute)
		a = reload(t, a, now, "", rid(1))
		got, err := a.Release(rid(1), ReasonExplicit, now)
		if err != nil || got.State != StateReleased || got.Reason != ReasonExplicit || got.Charged != 0 || !got.ClosedAt.Equal(now) {
			t.Fatalf("Release = %+v, %v", got, err)
		}
		if s := a.Snapshot(now); s.Remaining != 1000 || s.Reserved != 0 {
			t.Fatalf("snapshot = %+v", s)
		}
		ch := a.Changes()
		if len(ch.Movements) != 1 || ch.Movements[0] != (Movement{Kind: MovementRelease, Reservation: rid(1), HeldDelta: -300, At: now}) {
			t.Fatalf("movements = %+v", ch.Movements)
		}

		// A second Release is a no-op.
		a = reload(t, a, now, "", rid(1))
		again, err := a.Release(rid(1), ReasonFailed, now.Add(time.Second))
		if err != nil || again != got {
			t.Fatalf("second Release = %+v, %v; want %+v", again, err, got)
		}
		if ch := a.Changes(); len(ch.Movements) != 0 || len(ch.Updated) != 0 {
			t.Fatalf("no-op Release changed something: %+v", ch)
		}
	})

	t.Run("settled stays settled", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		settled, err := a.Settle(rid(1), usage(t, 100, false), t0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Release(rid(1), ReasonExplicit, t0)
		if err != nil || got != settled {
			t.Fatalf("Release of settled = %+v, %v", got, err)
		}
		assertTotals(t, a, 100, 0)
	})

	// Intentional (quota.proto): Release of a RELEASED reservation is a no-op, so the reason stays expired and a late
	// Settle still charges the work the provider did.
	t.Run("overdue is released by expiry first", func(t *testing.T) {
		a := newAccount(t, 1000)
		reserve(t, a, "k", 300, rid(1), t0)
		now := t0.Add(ttl)
		a = reload(t, a, now, "", rid(1))
		got, err := a.Release(rid(1), ReasonExplicit, now)
		if err != nil || got.Reason != ReasonExpired {
			t.Fatalf("Release = %+v, %v; want released by expiry", got, err)
		}
		if ch := a.Changes(); len(ch.Movements) != 1 || ch.Movements[0].Kind != MovementExpire {
			t.Fatalf("movements = %+v", ch.Movements)
		}
	})

	t.Run("unknown reservation", func(t *testing.T) {
		a := newAccount(t, 1000)
		if _, err := a.Release(rid(7), ReasonExplicit, t0); !errors.Is(err, ErrReservationNotFound) {
			t.Fatalf("err = %v, want ErrReservationNotFound", err)
		}
	})
}

func TestSnapshotNeverNegative(t *testing.T) {
	a, err := Restore(owner, policy(t, 100), 150, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Snapshot(t0); s.Remaining != 0 || s.Limit != 100 {
		t.Fatalf("snapshot = %+v", s)
	}
	if _, _, err := a.Reserve(request(t, "k", 1), rid(1), t0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}

	// Held alone above the limit, and both at the int64 boundary.
	r := validRecord()
	r.Amount = 200
	b, err := Restore(owner, policy(t, 100), 0, 200, []ReservationRecord{r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(t0); s.Remaining != 0 || s.Reserved != 200 {
		t.Fatalf("snapshot = %+v", s)
	}
	c, err := Restore(owner, policy(t, math.MaxInt64), math.MaxInt64-5, 5, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Snapshot(t0); s.Remaining != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestLargeAmountsDoNotOverflow(t *testing.T) {
	a := newAccount(t, math.MaxInt64)
	reserve(t, a, "a", math.MaxInt64-1, rid(1), t0)
	if _, _, err := a.Reserve(request(t, "b", 2), rid(2), t0); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
	reserve(t, a, "b", 1, rid(2), t0)
	if _, err := a.Settle(rid(1), usage(t, math.MaxInt64, false), t0); err != nil {
		t.Fatal(err)
	}
	if a.Used() != math.MaxInt64-1 || a.Held() != 1 {
		t.Fatalf("used %d held %d", a.Used(), a.Held())
	}
	if _, err := a.Settle(rid(2), usage(t, math.MaxInt64, false), t0); err != nil {
		t.Fatal(err)
	}
	if a.Used() != math.MaxInt64 || a.Held() != 0 || a.Snapshot(t0).Remaining != 0 {
		t.Fatalf("used %d held %d", a.Used(), a.Held())
	}
}

func TestRestoreRejects(t *testing.T) {
	p := policy(t, 1000)
	rec := validRecord()
	key := KeyRecord{Key: "k-1", Fingerprint: Fingerprint{Operation: OperationSummary, Alias: "fast", Amount: 100}, CreatedAt: t0, Attempts: 1, Latest: rid(1)}
	if _, err := Restore(owner, p, 0, 100, []ReservationRecord{rec}, []KeyRecord{key}); err != nil {
		t.Fatalf("valid account: %v", err)
	}

	withKey := func(mutate func(*KeyRecord)) []KeyRecord {
		k := key
		mutate(&k)
		return []KeyRecord{k}
	}
	cases := map[string]struct {
		owner      shared.APIKeyID
		used, held int64
		rs         []ReservationRecord
		keys       []KeyRecord
	}{
		"owner":              {owner: "x", held: 100, rs: []ReservationRecord{rec}},
		"negative used":      {owner: owner, used: -1},
		"negative held":      {owner: owner, held: -1},
		"total overflow":     {owner: owner, used: math.MaxInt64, held: 1},
		"holds above total":  {owner: owner, held: 99, rs: []ReservationRecord{rec}},
		"duplicate":          {owner: owner, held: 200, rs: []ReservationRecord{rec, rec}},
		"bad reservation":    {owner: owner, held: 100, rs: []ReservationRecord{{ID: rid(1)}}},
		"key without latest": {owner: owner, held: 0, keys: []KeyRecord{key}},
		"duplicate key":      {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: []KeyRecord{key, key}},
		"key attempt":        {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Attempts = 2 })},
		"key amount":         {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Fingerprint.Amount = 5 })},
		"key text":           {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Key = "k-2" })},
		"key operation":      {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Fingerprint.Operation = "" })},
		"key alias":          {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Fingerprint.Alias = "A" })},
		"key time":           {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.CreatedAt = time.Time{} })},
		"key zero attempts":  {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Attempts = 0 })},
		"key latest id":      {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Latest = "x" })},
		"key zero amount":    {owner: owner, held: 100, rs: []ReservationRecord{rec}, keys: withKey(func(k *KeyRecord) { k.Fingerprint.Amount = 0 })},
	}
	for name, c := range cases {
		if _, err := Restore(c.owner, p, c.used, c.held, c.rs, c.keys); !errors.Is(err, ErrInvalidRecord) {
			t.Errorf("%s: err = %v, want ErrInvalidRecord", name, err)
		}
	}
	if _, err := Restore(owner, Policy{}, 0, 0, nil, nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("zero policy: err = %v, want ErrInvalidPolicy", err)
	}
	if _, err := New(owner, Policy{}); !errors.Is(err, ErrInvalidPolicy) {
		t.Errorf("New with zero policy: err = %v, want ErrInvalidPolicy", err)
	}
}

// TestLedgerMatchesTotals runs a mixed sequence and checks that every movement is recorded and the sums match.
func TestLedgerMatchesTotals(t *testing.T) {
	a := newAccount(t, 10_000)
	reserve(t, a, "a", 1000, rid(1), t0)
	reserve(t, a, "b", 2000, rid(2), t0)
	reserve(t, a, "c", 3000, rid(3), t0)
	if _, err := a.Settle(rid(1), usage(t, 700, false), t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Release(rid(2), ReasonFailed, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Settle(rid(3), usage(t, 2500, false), t0.Add(ttl+time.Second)); err != nil { // expired, then settled late
		t.Fatal(err)
	}
	assertTotals(t, a, 3200, 0)
	kinds := map[MovementKind]int{}
	for _, m := range a.Changes().Movements {
		kinds[m.Kind]++
	}
	if kinds[MovementReserve] != 3 || kinds[MovementSettle] != 2 || kinds[MovementRelease] != 1 || kinds[MovementExpire] != 1 {
		t.Fatalf("movement kinds = %v", kinds)
	}
	if a.Owner() != owner {
		t.Fatalf("owner = %q", a.Owner())
	}
}
