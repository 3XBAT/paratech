package quota_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/cases/quota"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
)

const (
	alice shared.APIKeyID = "00000000000000a1"
	bob   shared.APIKeyID = "00000000000000b2"
	ttl                   = 10 * time.Minute
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc   *quota.Service
	store *testenv.QuotaStore
	clock *testenv.FakeClock
}

func newFixture(t *testing.T, limit int64) fixture {
	t.Helper()
	f := fixture{store: testenv.NewQuotaStore(), clock: testenv.NewFakeClock(t0)}
	svc, err := quota.New(f.store, f.clock, testenv.Random{}, config(limit))
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func config(limit int64) quota.Config {
	return quota.Config{Unit: "tokens", Limit: limit, ReservationTTL: ttl, KeyRetention: 24 * time.Hour}
}

func reserveCmd(owner shared.APIKeyID, key string, amount int64) quota.ReserveCommand {
	return quota.ReserveCommand{Owner: owner, IdempotencyKey: key, Operation: "summary", ModelAlias: "fast", Unit: "tokens", Amount: amount}
}

func (f fixture) reserve(t *testing.T, owner shared.APIKeyID, key string, amount int64) quota.Result {
	t.Helper()
	res, err := f.svc.Reserve(t.Context(), reserveCmd(owner, key, amount))
	if err != nil {
		t.Fatalf("Reserve(%s, %d): %v", key, amount, err)
	}
	return res
}

func (f fixture) remaining(t *testing.T, owner shared.APIKeyID) quota.Snapshot {
	t.Helper()
	s, err := f.svc.GetRemaining(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewValidates(t *testing.T) {
	store, clock := testenv.NewQuotaStore(), testenv.NewFakeClock(t0)
	for name, err := range map[string]error{
		"accounts":  second(quota.New(nil, clock, testenv.Random{}, config(1))),
		"clock":     second(quota.New(store, nil, testenv.Random{}, config(1))),
		"random":    second(quota.New(store, clock, nil, config(1))),
		"limit":     second(quota.New(store, clock, testenv.Random{}, config(0))),
		"unit":      second(quota.New(store, clock, testenv.Random{}, quota.Config{Unit: "credits", Limit: 1, ReservationTTL: ttl, KeyRetention: ttl})),
		"no config": second(quota.New(store, clock, testenv.Random{}, quota.Config{})),
	} {
		if !errors.Is(err, quota.ErrInvalidConfig) {
			t.Errorf("%s: err = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func second[T any](_ T, err error) error { return err }

func TestReserve(t *testing.T) {
	f := newFixture(t, 1000)
	res := f.reserve(t, alice, "job-1", 300)
	r := res.Reservation
	if res.Replayed || r.State != "reserved" || r.Unit != "tokens" || r.Reserved != 300 || r.Charged != 0 ||
		!r.ExpiresAt.Equal(t0.Add(ttl)) || shared.ReservationID(r.ID).Validate() != nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Quota != (quota.Snapshot{Unit: "tokens", Remaining: 700, Reserved: 300, Limit: 1000}) {
		t.Fatalf("snapshot = %+v", res.Quota)
	}

	// The same key replays and holds nothing extra.
	again := f.reserve(t, alice, "job-1", 300)
	if !again.Replayed || again.Reservation != r || again.Quota != res.Quota {
		t.Fatalf("replay = %+v", again)
	}
	if got := f.store.Ledger(alice); len(got) != 1 || got[0].Kind != account.MovementReserve {
		t.Fatalf("ledger = %+v", got)
	}

	// Another fingerprint conflicts.
	cmd := reserveCmd(alice, "job-1", 300)
	cmd.ModelAlias = "slow"
	if _, err := f.svc.Reserve(t.Context(), cmd); !errors.Is(err, account.ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestReserveQuotaExceeded(t *testing.T) {
	f := newFixture(t, 1000)
	f.reserve(t, alice, "a", 800)
	before := f.remaining(t, alice)

	_, err := f.svc.Reserve(t.Context(), reserveCmd(alice, "b", 201))
	var exceeded *quota.ExceededError
	if !errors.As(err, &exceeded) || !errors.Is(err, account.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want *ExceededError", err)
	}
	if exceeded.Quota != before {
		t.Fatalf("error snapshot %+v, want %+v", exceeded.Quota, before)
	}
	if f.remaining(t, alice) != before || len(f.store.Ledger(alice)) != 1 || len(f.store.Reservations(alice)) != 1 {
		t.Fatal("QUOTA_EXCEEDED stored something")
	}
}

func TestReserveRejectsInvalidArguments(t *testing.T) {
	f := newFixture(t, 1000)
	mutations := map[string]func(*quota.ReserveCommand){
		"owner":     func(c *quota.ReserveCommand) { c.Owner = "nope" },
		"key":       func(c *quota.ReserveCommand) { c.IdempotencyKey = "" },
		"operation": func(c *quota.ReserveCommand) { c.Operation = "unspecified" },
		"alias":     func(c *quota.ReserveCommand) { c.ModelAlias = "Fast" },
		"unit":      func(c *quota.ReserveCommand) { c.Unit = "" },
		"amount":    func(c *quota.ReserveCommand) { c.Amount = 0 },
	}
	for name, mutate := range mutations {
		cmd := reserveCmd(alice, "k", 10)
		mutate(&cmd)
		if _, err := f.svc.Reserve(t.Context(), cmd); !errors.Is(err, account.ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	if len(f.store.Ledger(alice)) != 0 {
		t.Fatal("an invalid Reserve stored something")
	}
}

// TestConcurrentReserveSameKey: parallel Reserve with one key hold the quota once (test step 2, on the fake).
func TestConcurrentReserveSameKey(t *testing.T) {
	f := newFixture(t, 1000)
	const n = 16
	results := make([]quota.Result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.svc.Reserve(context.WithoutCancel(t.Context()), reserveCmd(alice, "same", 100))
		}()
	}
	wg.Wait()
	replays := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Reserve #%d: %v", i, errs[i])
		}
		if results[i].Reservation.ID != results[0].Reservation.ID {
			t.Fatalf("two reservations for one key: %s and %s", results[i].Reservation.ID, results[0].Reservation.ID)
		}
		if results[i].Replayed {
			replays++
		}
	}
	if replays != n-1 {
		t.Fatalf("%d replays, want %d", replays, n-1)
	}
	if used, held := f.store.Totals(alice); used != 0 || held != 100 || len(f.store.Ledger(alice)) != 1 {
		t.Fatalf("used %d held %d ledger %d", used, held, len(f.store.Ledger(alice)))
	}
}

func TestConcurrentReserveDifferentKeysNeverOverdraw(t *testing.T) {
	f := newFixture(t, 1000)
	const n = 16 // 16 x 100 asks for 1600 of 1000
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Reserve(context.WithoutCancel(t.Context()), reserveCmd(alice, fmt.Sprintf("k-%d", i), 100))
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, account.ErrQuotaExceeded):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if used, held := f.store.Totals(alice); ok != 10 || held != 1000 || used != 0 {
		t.Fatalf("%d holds, held %d used %d; want 10 holds of 1000", ok, held, used)
	}
}

// TestSettleAndRelease: Settle returns the unused part, Release returns the whole hold (test step 4, on the fake).
func TestSettleAndRelease(t *testing.T) {
	f := newFixture(t, 1000)
	a := f.reserve(t, alice, "a", 300)
	b := f.reserve(t, alice, "b", 200)

	settled, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: a.Reservation.ID, Unit: "tokens", Actual: 120})
	if err != nil {
		t.Fatal(err)
	}
	if settled.Reservation.State != "settled" || settled.Reservation.Charged != 120 || settled.Reservation.Reserved != 300 {
		t.Fatalf("settled = %+v", settled.Reservation)
	}
	if settled.Quota != (quota.Snapshot{Unit: "tokens", Remaining: 680, Reserved: 200, Limit: 1000}) {
		t.Fatalf("after Settle: %+v", settled.Quota)
	}

	released, err := f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: b.Reservation.ID})
	if err != nil {
		t.Fatal(err)
	}
	if released.Reservation.State != "released" || released.Reservation.Charged != 0 {
		t.Fatalf("released = %+v", released.Reservation)
	}
	if released.Quota != (quota.Snapshot{Unit: "tokens", Remaining: 880, Reserved: 0, Limit: 1000}) || f.remaining(t, alice) != released.Quota {
		t.Fatalf("after Release: %+v", released.Quota)
	}

	// Repeats: Settle with the same usage replays, other usage conflicts; Release of a released one is a no-op;
	// Settle of an explicitly released one is refused.
	again, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: a.Reservation.ID, Unit: "tokens", Actual: 120})
	if err != nil || again.Reservation != settled.Reservation {
		t.Fatalf("repeat Settle = %+v, %v", again, err)
	}
	if _, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: a.Reservation.ID, Unit: "tokens", Actual: 121}); !errors.Is(err, account.ErrIdempotencyConflict) {
		t.Fatalf("Settle with other usage = %v, want conflict", err)
	}
	if res, err := f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: b.Reservation.ID}); err != nil || res.Reservation != released.Reservation {
		t.Fatalf("repeat Release = %+v, %v", res, err)
	}
	if res, err := f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: a.Reservation.ID}); err != nil || res.Reservation.State != "settled" {
		t.Fatalf("Release of settled = %+v, %v", res, err)
	}
	if _, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: b.Reservation.ID, Unit: "tokens", Actual: 1}); !errors.Is(err, account.ErrReleased) {
		t.Fatalf("Settle of released = %v, want ErrReleased", err)
	}
	if got := len(f.store.Ledger(alice)); got != 4 {
		t.Fatalf("%d ledger movements, want 4 (2 reserve, settle, release)", got)
	}
}

func TestSettleAndReleaseRejects(t *testing.T) {
	f := newFixture(t, 1000)
	res := f.reserve(t, alice, "a", 300)
	id := res.Reservation.ID
	for name, err := range map[string]error{
		"settle owner":  second(f.svc.Settle(t.Context(), quota.SettleCommand{Owner: "x", ReservationID: id, Unit: "tokens"})),
		"settle id":     second(f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: "x", Unit: "tokens"})),
		"settle unit":   second(f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: id, Unit: "credits"})),
		"settle actual": second(f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: id, Unit: "tokens", Actual: -1})),
		"release owner": second(f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: "", ReservationID: id})),
		"release id":    second(f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: id + "0"})),
		"remaining id":  second(f.svc.GetRemaining(t.Context(), "nope")),
	} {
		if !errors.Is(err, account.ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	unknown := "ffffffffffffffffffffffffffffffff"
	if _, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: unknown, Unit: "tokens"}); !errors.Is(err, account.ErrReservationNotFound) {
		t.Errorf("Settle unknown = %v", err)
	}
	if _, err := f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: unknown}); !errors.Is(err, account.ErrReservationNotFound) {
		t.Errorf("Release unknown = %v", err)
	}
}

func TestTenantIsolation(t *testing.T) {
	f := newFixture(t, 1000)
	a := f.reserve(t, alice, "shared-key", 300)
	b := f.reserve(t, bob, "shared-key", 300)
	if a.Reservation.ID == b.Reservation.ID || a.Replayed || b.Replayed {
		t.Fatalf("one key text for two testers must give two reservations: %+v %+v", a, b)
	}

	// Bob cannot address Alice's reservation; it looks like it does not exist.
	if _, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: bob, ReservationID: a.Reservation.ID, Unit: "tokens", Actual: 1}); !errors.Is(err, account.ErrReservationNotFound) {
		t.Fatalf("foreign Settle = %v, want ErrReservationNotFound", err)
	}
	if _, err := f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: bob, ReservationID: a.Reservation.ID}); !errors.Is(err, account.ErrReservationNotFound) {
		t.Fatalf("foreign Release = %v, want ErrReservationNotFound", err)
	}
	if s := f.remaining(t, alice); s.Reserved != 300 {
		t.Fatalf("Alice's quota changed: %+v", s)
	}
	if s := f.remaining(t, bob); s.Reserved != 300 {
		t.Fatalf("Bob's quota: %+v", s)
	}
}

func TestExpiry(t *testing.T) {
	f := newFixture(t, 1000)
	res := f.reserve(t, alice, "slow", 400)
	f.clock.Advance(ttl)

	// GetRemaining ignores the overdue hold and writes nothing.
	if s := f.remaining(t, alice); s.Remaining != 1000 || s.Reserved != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
	if _, held := f.store.Totals(alice); held != 400 || len(f.store.Ledger(alice)) != 1 {
		t.Fatal("GetRemaining wrote something")
	}

	// A late Settle still charges, after the lazy expiry released the hold.
	late, err := f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: res.Reservation.ID, Unit: "tokens", Actual: 350})
	if err != nil || late.Reservation.State != "settled" || late.Reservation.Charged != 350 {
		t.Fatalf("late Settle = %+v, %v", late, err)
	}
	ledger := f.store.Ledger(alice)
	if len(ledger) != 3 || ledger[1].Kind != account.MovementExpire || ledger[2].Kind != account.MovementSettle {
		t.Fatalf("ledger = %+v", ledger)
	}
	if used, held := f.store.Totals(alice); used != 350 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

func TestReserveAfterExpiryOpensANewAttempt(t *testing.T) {
	f := newFixture(t, 1000)
	first := f.reserve(t, alice, "k", 400)
	f.clock.Advance(ttl + time.Second)
	second := f.reserve(t, alice, "k", 400)
	if second.Reservation.ID == first.Reservation.ID || second.Replayed {
		t.Fatalf("expected a new attempt: %+v", second)
	}
	if second.Quota.Reserved != 400 {
		t.Fatalf("snapshot = %+v", second.Quota)
	}
}

func TestStoreAndRandomFailures(t *testing.T) {
	boom := errors.New("db down")
	f := newFixture(t, 1000)
	res := f.reserve(t, alice, "a", 10)
	f.store.Err = boom
	for name, err := range map[string]error{
		"reserve":   second(f.svc.Reserve(t.Context(), reserveCmd(alice, "b", 1))),
		"settle":    second(f.svc.Settle(t.Context(), quota.SettleCommand{Owner: alice, ReservationID: res.Reservation.ID, Unit: "tokens"})),
		"release":   second(f.svc.Release(t.Context(), quota.ReleaseCommand{Owner: alice, ReservationID: res.Reservation.ID})),
		"remaining": second(f.svc.GetRemaining(t.Context(), alice)),
	} {
		if !errors.Is(err, boom) {
			t.Errorf("%s: err = %v, want the store error", name, err)
		}
	}

	noEntropy := errors.New("no entropy")
	svc, err := quota.New(testenv.NewQuotaStore(), testenv.NewFakeClock(t0), testenv.Random{Err: noEntropy}, config(10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reserve(t.Context(), reserveCmd(alice, "a", 1)); !errors.Is(err, noEntropy) {
		t.Errorf("err = %v, want the random error", err)
	}
	svc, err = quota.New(testenv.NewQuotaStore(), testenv.NewFakeClock(t0), testenv.ZeroRandom{}, config(10))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reserve(t.Context(), reserveCmd(alice, "a", 1)); !errors.Is(err, account.ErrInvalidArgument) {
		t.Errorf("zero random: err = %v, want ErrInvalidArgument", err)
	}
}

func TestGetRemainingOfANewTester(t *testing.T) {
	f := newFixture(t, 5000)
	if s := f.remaining(t, bob); s != (quota.Snapshot{Unit: "tokens", Remaining: 5000, Reserved: 0, Limit: 5000}) {
		t.Fatalf("snapshot = %+v", s)
	}
	if f.store.Reservations(bob) != nil {
		t.Fatal("GetRemaining created an account")
	}
}
