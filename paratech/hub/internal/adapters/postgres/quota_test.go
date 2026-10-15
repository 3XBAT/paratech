package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

const quotaTTL = 10 * time.Minute

func quotaPolicy(t *testing.T, limit int64) account.Policy {
	t.Helper()
	p, err := account.NewPolicy("tokens", limit, quotaTTL, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// storedOwner creates an API key row and returns its id, the owner of a quota account.
func storedOwner(t *testing.T, db *DB) shared.APIKeyID {
	t.Helper()
	k, _ := newKey(t, "t1", keyT0)
	if err := db.APIKeys().Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	return k.ID()
}

func resID(n int) shared.ReservationID { return shared.ReservationID(fmt.Sprintf("%032x", n)) }

func reserveReq(t *testing.T, key string, amount int64) account.ReserveRequest {
	t.Helper()
	r, err := account.NewReserveRequest(key, "summary", "fast", "tokens", amount)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func scopeOf(owner shared.APIKeyID, p account.Policy, now time.Time) account.Scope {
	return account.Scope{Owner: owner, Policy: p, Now: now}
}

func count(t *testing.T, db *DB, table string, owner shared.APIKeyID) int {
	t.Helper()
	var n int
	if err := db.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE api_key_id = $1", owner.String()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestQuotaAccountsRoundTrip(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)

	scope := scopeOf(owner, p, keyT0)
	scope.Key = "job-1"
	err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
		_, _, err := a.Reserve(reserveReq(t, "job-1", 300), resID(1), keyT0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// The key record and its latest attempt come back; the same key replays.
	var replayed account.ReservationRecord
	err = repo.Modify(t.Context(), scope, func(a *account.Account) error {
		rec, again, err := a.Reserve(reserveReq(t, "job-1", 300), resID(2), keyT0)
		if err == nil && !again {
			err = errors.New("not replayed")
		}
		replayed = rec
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := account.ReservationRecord{ID: resID(1), Key: "job-1", Attempt: 1, Amount: 300, State: account.StateReserved,
		CreatedAt: keyT0, ExpiresAt: keyT0.Add(quotaTTL)}
	if replayed != want {
		t.Fatalf("restored reservation = %+v, want %+v", replayed, want)
	}

	// Settle by id, then read.
	settleScope := scopeOf(owner, p, keyT0.Add(time.Minute))
	settleScope.Reservation = resID(1)
	err = repo.Modify(t.Context(), settleScope, func(a *account.Account) error {
		u, err := account.NewUsage("tokens", 120, false)
		if err != nil {
			return err
		}
		_, err = a.Settle(resID(1), u, settleScope.Now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := repo.Read(t.Context(), scopeOf(owner, p, keyT0.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Snapshot(keyT0); s != (account.Snapshot{Unit: account.UnitTokens, Limit: 1000, Remaining: 880, Reserved: 0}) || a.Used() != 120 {
		t.Fatalf("snapshot = %+v, used %d", s, a.Used())
	}

	var state, reason string
	var charged, actual int64
	var closed time.Time
	err = db.pool.QueryRow(t.Context(), "SELECT state, release_reason, charged, actual, closed_at FROM quota_reservations WHERE id = $1",
		resID(1).String()).Scan(&state, &reason, &charged, &actual, &closed)
	if err != nil || state != "settled" || reason != "" || charged != 120 || actual != 120 || !closed.Equal(keyT0.Add(time.Minute)) {
		t.Fatalf("stored row: %s %q %d %d %v, %v", state, reason, charged, actual, closed, err)
	}
	assertLedger(t, db, owner, []string{"reserve", "settle"})
}

// assertLedger checks the kinds of the ledger rows of owner and that their deltas sum to the account totals.
func assertLedger(t *testing.T, db *DB, owner shared.APIKeyID, kinds []string) {
	t.Helper()
	rows, err := db.pool.Query(t.Context(), "SELECT kind FROM quota_ledger WHERE api_key_id = $1 ORDER BY id", owner.String())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		got = append(got, k)
	}
	rows.Close()
	if strings.Join(got, ",") != strings.Join(kinds, ",") {
		t.Fatalf("ledger kinds = %v, want %v", got, kinds)
	}

	var used, held, sumUsed, sumHeld int64
	err = db.pool.QueryRow(t.Context(), "SELECT a.used, a.held, COALESCE(sum(l.used_delta), 0), COALESCE(sum(l.held_delta), 0) "+
		"FROM quota_accounts a LEFT JOIN quota_ledger l ON l.api_key_id = a.api_key_id WHERE a.api_key_id = $1 GROUP BY a.used, a.held",
		owner.String()).Scan(&used, &held, &sumUsed, &sumHeld)
	if err != nil {
		t.Fatal(err)
	}
	if used != sumUsed || held != sumHeld {
		t.Fatalf("totals used %d held %d, ledger sums used %d held %d", used, held, sumUsed, sumHeld)
	}
}

func TestQuotaAccountsRollback(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)

	boom := errors.New("fn failed")
	err := repo.Modify(t.Context(), scopeOf(owner, p, keyT0), func(a *account.Account) error {
		if _, _, err := a.Reserve(reserveReq(t, "k", 10), resID(1), keyT0); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || err != boom {
		t.Fatalf("err = %v, want the error of fn unchanged", err)
	}
	for _, table := range []string{"quota_accounts", "quota_reservations", "quota_idempotency_keys", "quota_ledger"} {
		if n := count(t, db, table, owner); n != 0 {
			t.Errorf("%s has %d rows after a failed command", table, n)
		}
	}

	// A domain error also passes through unchanged.
	err = repo.Modify(t.Context(), scopeOf(owner, p, keyT0), func(a *account.Account) error {
		_, _, err := a.Reserve(reserveReq(t, "k", 1001), resID(1), keyT0)
		return err
	})
	if !errors.Is(err, account.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want ErrQuotaExceeded", err)
	}
}

func TestQuotaAccountsReadWritesNothing(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 500)

	a, err := repo.Read(t.Context(), scopeOf(owner, p, keyT0))
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Snapshot(keyT0); s.Remaining != 500 || s.Limit != 500 {
		t.Fatalf("snapshot = %+v", s)
	}
	if n := count(t, db, "quota_accounts", owner); n != 0 {
		t.Fatalf("Read created %d account rows", n)
	}

	// An overdue hold is loaded and counts as released, but stays stored as it is.
	if err := repo.Modify(t.Context(), scopeOf(owner, p, keyT0), func(a *account.Account) error {
		_, _, err := a.Reserve(reserveReq(t, "k", 200), resID(1), keyT0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	late := keyT0.Add(quotaTTL)
	a, err = repo.Read(t.Context(), scopeOf(owner, p, late))
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Snapshot(late); s.Remaining != 500 || s.Reserved != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
	var held int64
	if err := db.pool.QueryRow(t.Context(), "SELECT held FROM quota_accounts WHERE api_key_id = $1", owner.String()).Scan(&held); err != nil || held != 200 {
		t.Fatalf("held = %d, %v; Read must not write", held, err)
	}
}

func TestQuotaAccountsUnknownOwner(t *testing.T) {
	db := migratedDB(t)
	err := db.QuotaAccounts().Modify(t.Context(), scopeOf("0123456789abcdef", quotaPolicy(t, 10), keyT0), func(*account.Account) error {
		t.Fatal("fn must not run")
		return nil
	})
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestQuotaAccountsScopeIsPerOwner(t *testing.T) {
	db := migratedDB(t)
	repo, p := db.QuotaAccounts(), quotaPolicy(t, 1000)
	alice, bob := storedOwner(t, db), storedOwner(t, db)

	scope := scopeOf(alice, p, keyT0)
	scope.Key = "same"
	if err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
		_, _, err := a.Reserve(reserveReq(t, "same", 100), resID(1), keyT0)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Bob's account sees neither Alice's key nor her reservation.
	scope = scopeOf(bob, p, keyT0)
	scope.Key, scope.Reservation = "same", resID(1)
	err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
		if _, err := a.Release(resID(1), account.ReasonExplicit, keyT0); !errors.Is(err, account.ErrReservationNotFound) {
			return fmt.Errorf("foreign Release = %w", err)
		}
		rec, replayed, err := a.Reserve(reserveReq(t, "same", 100), resID(2), keyT0)
		if err == nil && (replayed || rec.ID != resID(2)) {
			err = errors.New("Bob replayed Alice's reservation")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if count(t, db, "quota_reservations", alice) != 1 || count(t, db, "quota_reservations", bob) != 1 {
		t.Fatal("each tester must have exactly one reservation")
	}
}

func TestQuotaAccountsCorruptRows(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)
	if err := repo.Modify(t.Context(), scopeOf(owner, p, keyT0), func(a *account.Account) error {
		_, _, err := a.Reserve(reserveReq(t, "k", 100), resID(1), keyT0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The totals no longer cover the open reservation.
	if _, err := db.pool.Exec(t.Context(), "UPDATE quota_accounts SET held = 0 WHERE api_key_id = $1", owner.String()); err != nil {
		t.Fatal(err)
	}
	scope := scopeOf(owner, p, keyT0)
	scope.Reservation = resID(1)
	err := repo.Modify(t.Context(), scope, func(*account.Account) error { return nil })
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

// TestQuotaSchemaConstraints checks that the database itself refuses rows the domain never writes.
func TestQuotaSchemaConstraints(t *testing.T) {
	db := migratedDB(t)
	owner := storedOwner(t, db)
	if _, err := db.pool.Exec(t.Context(), "INSERT INTO quota_accounts (api_key_id, used, held, created_at, updated_at) VALUES ($1, 0, 0, $2, $2)",
		owner.String(), keyT0); err != nil {
		t.Fatal(err)
	}
	insert := func(id, state, reason string, amount, charged, actual int64, closed any) error {
		_, err := db.pool.Exec(t.Context(), "INSERT INTO quota_reservations ("+reservationColumns+", api_key_id) "+
			"VALUES ($1, 'k', 1, $2, $3, $4, $5, $6, false, $7, $8, $9, $10)",
			id, amount, state, reason, charged, actual, keyT0, keyT0.Add(time.Minute), closed, owner.String())
		return err
	}
	if err := insert(resID(1).String(), "reserved", "", 10, 0, 0, nil); err != nil {
		t.Fatalf("valid row: %v", err)
	}
	for name, err := range map[string]error{
		"negative amount":    insert(resID(2).String(), "reserved", "", -1, 0, 0, nil),
		"unknown state":      insert(resID(3).String(), "lost", "", 1, 0, 0, nil),
		"reserved with cost": insert(resID(4).String(), "reserved", "", 1, 1, 1, nil),
		"charged > actual":   insert(resID(5).String(), "settled", "", 1, 2, 1, keyT0),
		"released no reason": insert(resID(6).String(), "released", "", 1, 0, 0, keyT0),
		"bad id":             insert("XYZ", "reserved", "", 1, 0, 0, nil),
		"duplicate attempt":  insert(resID(7).String(), "reserved", "", 1, 0, 0, nil),
	} {
		if err == nil {
			t.Errorf("%s: the row was accepted", name)
		}
	}
	for name, sql := range map[string]string{
		"negative used":     "UPDATE quota_accounts SET used = -1 WHERE api_key_id = $1",
		"ledger bad delta":  "INSERT INTO quota_ledger (api_key_id, reservation_id, kind, held_delta, used_delta, created_at) VALUES ($1, '" + resID(1).String() + "', 'reserve', -5, 0, now())",
		"ledger bad kind":   "INSERT INTO quota_ledger (api_key_id, reservation_id, kind, held_delta, used_delta, created_at) VALUES ($1, '" + resID(1).String() + "', 'gift', 5, 0, now())",
		"ledger foreign id": "INSERT INTO quota_ledger (api_key_id, reservation_id, kind, held_delta, used_delta, created_at) VALUES ($1, '" + resID(99).String() + "', 'reserve', 5, 0, now())",
	} {
		if _, err := db.pool.Exec(t.Context(), sql, owner.String()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestQuotaAccountsClosed(t *testing.T) {
	db := migratedDB(t)
	repo := db.QuotaAccounts()
	db.Close()
	scope := scopeOf("0123456789abcdef", quotaPolicy(t, 10), keyT0)
	if err := repo.Modify(t.Context(), scope, func(*account.Account) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Errorf("Modify = %v, want ErrClosed", err)
	}
	if _, err := repo.Read(t.Context(), scope); !errors.Is(err, ErrClosed) {
		t.Errorf("Read = %v, want ErrClosed", err)
	}
}
