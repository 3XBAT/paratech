package tests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	connectrpc "connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"paratech/hub/config"
	"paratech/hub/internal/adapters/postgres"
	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/cases/quota"
	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// The Postgres adapter implements the port of the quota use case.
var _ quota.Accounts = (*postgres.QuotaAccounts)(nil)

const quotaTTL = 10 * time.Minute

var quotaT0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// quotaEnv is the real quota use case on a fresh Postgres schema with a fake clock.
type quotaEnv struct {
	svc   *quota.Service
	db    *postgres.DB
	conn  *pgx.Conn
	clock *testenv.FakeClock
}

func newQuotaEnv(t *testing.T, limit int64) quotaEnv {
	t.Helper()
	dbURL := testenv.PostgresURL(t)
	db, err := postgres.Open(t.Context(), dbURL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	clock := testenv.NewFakeClock(quotaT0)
	svc, err := quota.New(db.QuotaAccounts(), clock, testenv.Random{},
		quota.Config{Unit: "tokens", Limit: limit, ReservationTTL: quotaTTL, KeyRetention: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return quotaEnv{svc: svc, db: db, conn: conn, clock: clock}
}

// tester issues an API key and returns its id, the owner of a quota account.
func (e quotaEnv) tester(t *testing.T) shared.APIKeyID {
	t.Helper()
	svc, err := keys.New(e.db.APIKeys(), e.clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(t.Context(), "tester")
	if err != nil {
		t.Fatal(err)
	}
	return shared.APIKeyID(issued.ID)
}

func reserveCmd(owner shared.APIKeyID, key string, amount int64) quota.ReserveCommand {
	return quota.ReserveCommand{Owner: owner, IdempotencyKey: key, Operation: "summary", ModelAlias: "fast", Unit: "tokens", Amount: amount}
}

func (e quotaEnv) reserve(t *testing.T, owner shared.APIKeyID, key string, amount int64) quota.Result {
	t.Helper()
	res, err := e.svc.Reserve(t.Context(), reserveCmd(owner, key, amount))
	if err != nil {
		t.Fatalf("Reserve(%s, %d): %v", key, amount, err)
	}
	return res
}

func (e quotaEnv) settle(owner shared.APIKeyID, id string, actual int64, partial bool) (quota.Result, error) {
	return e.svc.Settle(context.Background(), quota.SettleCommand{Owner: owner, ReservationID: id, Unit: "tokens", Actual: actual, Partial: partial})
}

func (e quotaEnv) release(owner shared.APIKeyID, id string) (quota.Result, error) {
	return e.svc.Release(context.Background(), quota.ReleaseCommand{Owner: owner, ReservationID: id})
}

func (e quotaEnv) remaining(t *testing.T, owner shared.APIKeyID) quota.Snapshot {
	t.Helper()
	s, err := e.svc.GetRemaining(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ledgerKinds returns the number of ledger rows of owner by kind.
func (e quotaEnv) ledgerKinds(t *testing.T, owner shared.APIKeyID) map[string]int {
	t.Helper()
	rows, err := e.conn.Query(t.Context(), "SELECT kind, count(*) FROM quota_ledger WHERE api_key_id = $1 GROUP BY kind", owner.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			t.Fatal(err)
		}
		out[kind] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e quotaEnv) rowCount(t *testing.T, table string, owner shared.APIKeyID) int {
	t.Helper()
	var n int
	if err := e.conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE api_key_id = $1", owner.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// assertConsistent checks the stored invariants of owner's account: held is the sum of the open reservations, used
// the sum of the charges, and the ledger deltas sum to both totals.
func (e quotaEnv) assertConsistent(t *testing.T, owner shared.APIKeyID) (used, held int64) {
	t.Helper()
	var openSum, chargedSum, ledgerHeld, ledgerUsed int64
	err := e.conn.QueryRow(t.Context(), `
		SELECT a.used, a.held,
		       (SELECT COALESCE(sum(amount), 0) FROM quota_reservations r WHERE r.api_key_id = a.api_key_id AND r.state = 'reserved'),
		       (SELECT COALESCE(sum(charged), 0) FROM quota_reservations r WHERE r.api_key_id = a.api_key_id),
		       (SELECT COALESCE(sum(held_delta), 0) FROM quota_ledger l WHERE l.api_key_id = a.api_key_id),
		       (SELECT COALESCE(sum(used_delta), 0) FROM quota_ledger l WHERE l.api_key_id = a.api_key_id)
		FROM quota_accounts a WHERE a.api_key_id = $1`, owner.String()).
		Scan(&used, &held, &openSum, &chargedSum, &ledgerHeld, &ledgerUsed)
	if err != nil {
		t.Fatal(err)
	}
	if held != openSum || used != chargedSum || held != ledgerHeld || used != ledgerUsed {
		t.Fatalf("inconsistent account: used %d held %d, open reservations %d, charges %d, ledger held %d used %d",
			used, held, openSum, chargedSum, ledgerHeld, ledgerUsed)
	}
	return used, held
}

// TestQuotaConcurrentReserveSameKeyAgainstPostgres is test step 2: parallel Reserve calls with one idempotency key
// charge the quota once.
func TestQuotaConcurrentReserveSameKeyAgainstPostgres(t *testing.T) {
	for _, n := range []int{2, 16} {
		t.Run(fmt.Sprintf("%d callers", n), func(t *testing.T) {
			e := newQuotaEnv(t, 1000)
			owner := e.tester(t)

			results := make([]quota.Result, n)
			errs := make([]error, n)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					results[i], errs[i] = e.svc.Reserve(context.Background(), reserveCmd(owner, "job-42", 300))
				}()
			}
			close(start)
			wg.Wait()

			fresh := 0
			for i := range n {
				if errs[i] != nil {
					t.Fatalf("Reserve #%d: %v", i, errs[i])
				}
				if results[i].Reservation.ID != results[0].Reservation.ID {
					t.Fatalf("one key got two reservations: %s and %s", results[0].Reservation.ID, results[i].Reservation.ID)
				}
				if !results[i].Replayed {
					fresh++
				}
			}
			if fresh != 1 {
				t.Fatalf("%d calls created a reservation, want exactly 1", fresh)
			}
			if used, held := e.assertConsistent(t, owner); used != 0 || held != 300 {
				t.Fatalf("used %d held %d, want the amount held once", used, held)
			}
			if k := e.ledgerKinds(t, owner); k["reserve"] != 1 || len(k) != 1 {
				t.Fatalf("ledger = %v, want exactly one reserve row", k)
			}
			if s := e.remaining(t, owner); s.Remaining != 700 || s.Reserved != 300 {
				t.Fatalf("snapshot = %+v", s)
			}
		})
	}
}

// TestQuotaConcurrentReserveDifferentKeysAgainstPostgres: parallel holds with distinct keys never overdraw the limit.
func TestQuotaConcurrentReserveDifferentKeysAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	owner := e.tester(t)

	const n = 24 // 24 x 100 asks for 2400 of 1000
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = e.svc.Reserve(context.Background(), reserveCmd(owner, fmt.Sprintf("job-%d", i), 100))
		}()
	}
	close(start)
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
	used, held := e.assertConsistent(t, owner)
	if ok != 10 || held != 1000 || used != 0 {
		t.Fatalf("%d holds succeeded, held %d used %d; want 10 holds of exactly the limit", ok, held, used)
	}
	if got := e.rowCount(t, "quota_reservations", owner); got != ok {
		t.Fatalf("%d reservations stored for %d successful holds", got, ok)
	}
}

// TestQuotaReserveOverRemainingAgainstPostgres is test step 3: Reserve above the remaining quota is QUOTA_EXCEEDED
// and the remaining quota does not change.
func TestQuotaReserveOverRemainingAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	owner := e.tester(t)
	e.reserve(t, owner, "first", 700)
	before := e.remaining(t, owner)
	rows := map[string]int{}
	for _, table := range []string{"quota_reservations", "quota_idempotency_keys", "quota_ledger"} {
		rows[table] = e.rowCount(t, table, owner)
	}

	_, err := e.svc.Reserve(t.Context(), reserveCmd(owner, "second", 301))
	var exceeded *quota.ExceededError
	if !errors.As(err, &exceeded) || !errors.Is(err, account.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want QUOTA_EXCEEDED", err)
	}
	if exceeded.Quota != before {
		t.Fatalf("error snapshot = %+v, want %+v", exceeded.Quota, before)
	}
	if after := e.remaining(t, owner); after != before || after.Remaining != 300 {
		t.Fatalf("remaining changed: %+v -> %+v", before, after)
	}
	for table, n := range rows {
		if got := e.rowCount(t, table, owner); got != n {
			t.Errorf("%s: %d rows, want %d (nothing written)", table, got, n)
		}
	}

	// The same key retried later with quota available opens its first attempt.
	if res := e.reserve(t, owner, "second", 300); res.Replayed || res.Quota.Remaining != 0 {
		t.Fatalf("retry = %+v", res)
	}
}

// TestQuotaSettleAndReleaseAgainstPostgres is test step 4: Settle returns the unused part of a reservation and Release
// returns the whole of it.
func TestQuotaSettleAndReleaseAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	owner := e.tester(t)
	a := e.reserve(t, owner, "a", 400)
	b := e.reserve(t, owner, "b", 300)
	if s := e.remaining(t, owner); s.Remaining != 300 {
		t.Fatalf("after two holds: %+v", s)
	}

	settled, err := e.settle(owner, a.Reservation.ID, 150, false)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Reservation.State != "settled" || settled.Reservation.Charged != 150 || settled.Quota.Remaining != 550 {
		t.Fatalf("Settle = %+v, want 250 of 400 returned", settled)
	}
	released, err := e.release(owner, b.Reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.Reservation.State != "released" || released.Reservation.Charged != 0 || released.Quota.Remaining != 850 {
		t.Fatalf("Release = %+v, want the whole 300 returned", released)
	}
	if s := e.remaining(t, owner); s != (quota.Snapshot{Unit: "tokens", Remaining: 850, Reserved: 0, Limit: 1000}) {
		t.Fatalf("snapshot = %+v", s)
	}
	if used, held := e.assertConsistent(t, owner); used != 150 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
	if k := e.ledgerKinds(t, owner); k["reserve"] != 2 || k["settle"] != 1 || k["release"] != 1 || len(k) != 3 {
		t.Fatalf("ledger = %v", k)
	}
}

// TestQuotaConcurrentSettleAndReleaseAgainstPostgres: a racing Settle and Release of one reservation have exactly one
// outcome, and parallel identical Settle calls charge once.
func TestQuotaConcurrentSettleAndReleaseAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 100_000)
	owner := e.tester(t)

	settledWins, releasedWins := 0, 0
	for i := range 10 {
		res := e.reserve(t, owner, fmt.Sprintf("race-%d", i), 1000)
		var settleErr, releaseErr error
		var settleRes, releaseRes quota.Result
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			settleRes, settleErr = e.settle(owner, res.Reservation.ID, 600, false)
		}()
		go func() { defer wg.Done(); <-start; releaseRes, releaseErr = e.release(owner, res.Reservation.ID) }()
		close(start)
		wg.Wait()

		if releaseErr != nil {
			t.Fatalf("Release: %v", releaseErr)
		}
		switch {
		case settleErr == nil: // Settle came first; Release then reports SETTLED and changes nothing
			if settleRes.Reservation.Charged != 600 || releaseRes.Reservation.State != "settled" {
				t.Fatalf("settle won but: settle %+v, release %+v", settleRes.Reservation, releaseRes.Reservation)
			}
			settledWins++
		case errors.Is(settleErr, account.ErrReleased): // Release came first; Settle is refused
			if releaseRes.Reservation.State != "released" {
				t.Fatalf("release won but reports %+v", releaseRes.Reservation)
			}
			releasedWins++
		default:
			t.Fatalf("Settle: %v", settleErr)
		}
	}
	used, held := e.assertConsistent(t, owner)
	if held != 0 || used != int64(settledWins)*600 {
		t.Fatalf("used %d held %d after %d settled and %d released", used, held, settledWins, releasedWins)
	}

	// Identical Settle calls in parallel: one charge, one ledger row, the same outcome for all.
	res := e.reserve(t, owner, "dup-settle", 1000)
	const n = 8
	results := make([]quota.Result, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = e.settle(owner, res.Reservation.ID, 700, false)
		}()
	}
	close(start)
	wg.Wait()
	for i := range n {
		if errs[i] != nil || results[i].Reservation != results[0].Reservation || results[i].Reservation.Charged != 700 {
			t.Fatalf("Settle #%d = %+v, %v", i, results[i].Reservation, errs[i])
		}
	}
	usedAfter, _ := e.assertConsistent(t, owner)
	if usedAfter != used+700 {
		t.Fatalf("used %d after parallel Settle, want %d (charged once)", usedAfter, used+700)
	}
	if k := e.ledgerKinds(t, owner); k["settle"] != settledWins+1 {
		t.Fatalf("ledger = %v, want %d settle rows", k, settledWins+1)
	}
}

// TestQuotaKeyReplayAgainstPostgres covers the key life cycle across Settle and Release.
func TestQuotaKeyReplayAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 10_000)
	owner := e.tester(t)

	// Complete Settle: the key replays the settled reservation.
	done := e.reserve(t, owner, "complete", 500)
	if _, err := e.settle(owner, done.Reservation.ID, 200, false); err != nil {
		t.Fatal(err)
	}
	replay := e.reserve(t, owner, "complete", 500)
	if !replay.Replayed || replay.Reservation.ID != done.Reservation.ID || replay.Reservation.State != "settled" || replay.Reservation.Charged != 200 {
		t.Fatalf("replay after complete Settle = %+v", replay)
	}

	// Partial Settle and Release end the attempt: the key opens a new one.
	partial := e.reserve(t, owner, "partial", 500)
	if _, err := e.settle(owner, partial.Reservation.ID, 100, true); err != nil {
		t.Fatal(err)
	}
	next := e.reserve(t, owner, "partial", 500)
	if next.Replayed || next.Reservation.ID == partial.Reservation.ID {
		t.Fatalf("after partial Settle = %+v, want a new attempt", next)
	}
	released := e.reserve(t, owner, "released", 500)
	if _, err := e.release(owner, released.Reservation.ID); err != nil {
		t.Fatal(err)
	}
	again := e.reserve(t, owner, "released", 500)
	if again.Replayed || again.Reservation.ID == released.Reservation.ID {
		t.Fatalf("after Release = %+v, want a new attempt", again)
	}

	// Another amount for a used key conflicts, in every state.
	for _, key := range []string{"complete", "partial", "released"} {
		if _, err := e.svc.Reserve(t.Context(), reserveCmd(owner, key, 501)); !errors.Is(err, account.ErrIdempotencyConflict) {
			t.Errorf("%s with another amount: err = %v, want ErrIdempotencyConflict", key, err)
		}
	}
	var attempts int
	if err := e.conn.QueryRow(t.Context(), "SELECT attempts FROM quota_idempotency_keys WHERE api_key_id = $1 AND idempotency_key = 'partial'",
		owner.String()).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("attempts = %d, %v; want 2", attempts, err)
	}
	e.assertConsistent(t, owner)
}

// TestQuotaKeyLivePastRetentionAgainstPostgres: a key whose latest attempt is still RESERVED stays live after its
// retention ends, so one key never gets a second hold.
func TestQuotaKeyLivePastRetentionAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	owner := e.tester(t)
	const retention = 24 * time.Hour

	first := e.reserve(t, owner, "k", 100)
	if _, err := e.release(owner, first.Reservation.ID); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(retention - quotaTTL/2)
	second := e.reserve(t, owner, "k", 100)
	if second.Replayed || second.Reservation.ID == first.Reservation.ID {
		t.Fatalf("attempt 2 = %+v, want a new attempt", second)
	}

	e.clock.Advance(quotaTTL / 2) // the key record is past its retention, attempt 2 is not expired
	if _, err := e.svc.Reserve(t.Context(), reserveCmd(owner, "k", 200)); !errors.Is(err, account.ErrIdempotencyConflict) {
		t.Fatalf("another amount past retention: err = %v, want ErrIdempotencyConflict", err)
	}
	replay := e.reserve(t, owner, "k", 100)
	if !replay.Replayed || replay.Reservation.ID != second.Reservation.ID {
		t.Fatalf("same amount past retention = %+v, want a replay of %s", replay, second.Reservation.ID)
	}
	if used, held := e.assertConsistent(t, owner); used != 0 || held != 100 {
		t.Fatalf("used %d held %d, want one hold of 100", used, held)
	}
	if n := e.rowCount(t, "quota_reservations", owner); n != 2 {
		t.Fatalf("%d reservations stored, want 2", n)
	}
}

// TestQuotaExpiryAgainstPostgres: an overdue reservation is released lazily, a late Settle still charges, and
// GetRemaining ignores overdue holds without writing.
func TestQuotaExpiryAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	owner := e.tester(t)
	slow := e.reserve(t, owner, "slow", 600)
	e.clock.Advance(quotaTTL)

	if s := e.remaining(t, owner); s.Remaining != 1000 || s.Reserved != 0 {
		t.Fatalf("GetRemaining with an overdue hold = %+v", s)
	}
	if _, held := e.assertConsistent(t, owner); held != 600 {
		t.Fatalf("GetRemaining wrote: held %d", held)
	}

	// Another command releases the overdue hold by expiry first.
	other := e.reserve(t, owner, "other", 1000)
	if other.Quota.Remaining != 0 {
		t.Fatalf("Reserve after expiry = %+v", other)
	}
	if k := e.ledgerKinds(t, owner); k["expire"] != 1 {
		t.Fatalf("ledger = %v, want one expire row", k)
	}

	// The late Settle is charged what the account can bear (nothing is left: "other" holds everything).
	late, err := e.settle(owner, slow.Reservation.ID, 500, false)
	if err != nil || late.Reservation.State != "settled" || late.Reservation.Charged != 0 {
		t.Fatalf("late Settle = %+v, %v", late, err)
	}
	if _, err := e.release(owner, other.Reservation.ID); err != nil {
		t.Fatal(err)
	}

	// With room left, a late Settle charges its actual usage.
	slow2 := e.reserve(t, owner, "slow-2", 300)
	e.clock.Advance(quotaTTL + time.Second)
	late2, err := e.settle(owner, slow2.Reservation.ID, 250, false)
	if err != nil || late2.Reservation.Charged != 250 || late2.Quota.Remaining != 750 {
		t.Fatalf("late Settle with room = %+v, %v", late2, err)
	}
	// It is settled once: a repeat replays, Release changes nothing.
	if again, err := e.settle(owner, slow2.Reservation.ID, 250, false); err != nil || again.Reservation != late2.Reservation {
		t.Fatalf("repeat = %+v, %v", again, err)
	}
	if used, held := e.assertConsistent(t, owner); used != 250 || held != 0 {
		t.Fatalf("used %d held %d", used, held)
	}
}

// TestQuotaTenantIsolationAgainstPostgres: a tester cannot settle or release another one's reservation, and the same
// key text of two testers names two unrelated keys.
func TestQuotaTenantIsolationAgainstPostgres(t *testing.T) {
	e := newQuotaEnv(t, 1000)
	alice, bob := e.tester(t), e.tester(t)

	a := e.reserve(t, alice, "job-1", 400)
	b := e.reserve(t, bob, "job-1", 400)
	if b.Replayed || a.Reservation.ID == b.Reservation.ID {
		t.Fatalf("Bob's key replayed Alice's reservation: %+v %+v", a, b)
	}

	if _, err := e.settle(bob, a.Reservation.ID, 10, false); !errors.Is(err, account.ErrReservationNotFound) {
		t.Fatalf("Bob settles Alice's reservation: %v, want ErrReservationNotFound", err)
	}
	if _, err := e.release(bob, a.Reservation.ID); !errors.Is(err, account.ErrReservationNotFound) {
		t.Fatalf("Bob releases Alice's reservation: %v, want ErrReservationNotFound", err)
	}
	if s := e.remaining(t, alice); s.Reserved != 400 || s.Remaining != 600 {
		t.Fatalf("Alice's quota changed: %+v", s)
	}
	if _, err := e.release(alice, a.Reservation.ID); err != nil {
		t.Fatal(err)
	}
	if s := e.remaining(t, bob); s.Reserved != 400 {
		t.Fatalf("Bob's quota changed: %+v", s)
	}
	e.assertConsistent(t, alice)
	e.assertConsistent(t, bob)
}

// TestGetRemainingOverHTTPAgainstPostgres: the public GetRemaining answers the caller's snapshot (AC-12) with the
// configured limit; the internal RPCs stay unrouted.
func TestGetRemainingOverHTTPAgainstPostgres(t *testing.T) {
	dbURL := testenv.PostgresURL(t)
	key := issuedKey(t, runKeyCommand(t, dbURL, config.CommandKeysCreate, "tester-02", ""))
	base := startHub(t, dbURL)

	req := connectrpc.NewRequest(&hubv1.GetRemainingRequest{})
	req.Header().Set("Authorization", "Bearer "+key)
	resp, err := hubv1connect.NewQuotaServiceClient(http.DefaultClient, base).GetRemaining(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	q := resp.Msg.GetQuota()
	limit := config.Default().QuotaLimit
	if q.GetLimit().GetUnit() != hubv1.QuotaUnit_QUOTA_UNIT_TOKENS || q.GetLimit().GetValue() != limit ||
		q.GetRemaining().GetValue() != limit || q.GetRemaining().GetUnit() != hubv1.QuotaUnit_QUOTA_UNIT_TOKENS ||
		q.GetReserved().GetValue() != 0 || q.GetPeriodEndsAt() != nil {
		t.Fatalf("snapshot = %v", q)
	}

	for _, p := range []string{hubv1connect.QuotaServiceReserveProcedure, hubv1connect.QuotaServiceSettleProcedure, hubv1connect.QuotaServiceReleaseProcedure} {
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+p, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+key)
		r, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("%s: HTTP %d, want 404", p, r.StatusCode)
		}
	}
}
