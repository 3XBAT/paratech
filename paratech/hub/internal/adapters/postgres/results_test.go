package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/domain/quota/account"
	"paratech/hub/internal/domain/shared"
)

var requestHash = strings.Repeat("ab", 32)

// startAndComplete runs a gateway attempt of key through the repository: Start, then Complete with output.
func startAndComplete(t *testing.T, repo *QuotaAccounts, owner shared.APIKeyID, p account.Policy, key string, id shared.ReservationID, output string, at time.Time) {
	t.Helper()
	req, err := reserveReq(t, key, 300).WithRequestHash(requestHash)
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeOf(owner, p, at)
	scope.Key = account.IdempotencyKey(key)
	if err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
		_, _, err := a.Start(req, id, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scope.Reservation = id
	if err := repo.Modify(t.Context(), scope, func(a *account.Account) error {
		u, err := account.NewUsage("tokens", 15, false)
		if err != nil {
			return err
		}
		res, err := account.NewResult(output, 10, 5)
		if err != nil {
			return err
		}
		_, err = a.Complete(id, u, res, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// replay runs Start again and returns the stored result it replays.
func replay(t *testing.T, repo *QuotaAccounts, owner shared.APIKeyID, p account.Policy, key string, at time.Time) (account.Result, error) {
	t.Helper()
	req, err := reserveReq(t, key, 300).WithRequestHash(requestHash)
	if err != nil {
		t.Fatal(err)
	}
	scope := scopeOf(owner, p, at)
	scope.Key = account.IdempotencyKey(key)
	var res account.Result
	err = repo.Modify(t.Context(), scope, func(a *account.Account) error {
		var err error
		_, res, err = a.Start(req, resID(99), at)
		return err
	})
	return res, err
}

func TestQuotaAccountsStoreResults(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)

	// NUL and an empty answer are stored as they are (bytea).
	for i, output := range []string{"answer with \x00 NUL", ""} {
		key := []string{"job-1", "job-2"}[i]
		startAndComplete(t, repo, owner, p, key, resID(i+1), output, keyT0)
		res, err := replay(t, repo, owner, p, key, keyT0.Add(time.Minute))
		if err != nil || !res.Stored || res.Output != output || res.InputTokens != 10 || res.OutputTokens != 5 {
			t.Fatalf("%s: replay = %v, %v", key, res, err)
		}
	}

	var hash string
	if err := db.pool.QueryRow(t.Context(), "SELECT request_hash FROM quota_idempotency_keys WHERE api_key_id = $1 AND idempotency_key = 'job-1'",
		owner.String()).Scan(&hash); err != nil || hash != requestHash {
		t.Fatalf("stored hash %q, %v", hash, err)
	}
	if n := count(t, db, "quota_ledger", owner); n != 4 {
		t.Fatalf("ledger rows = %d, want 4 (no row for a replay)", n)
	}
}

func TestQuotaAccountsPurgeResults(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)
	startAndComplete(t, repo, owner, p, "old", resID(1), "old answer", keyT0)
	startAndComplete(t, repo, owner, p, "new", resID(2), "new answer", keyT0.Add(time.Hour))

	n, err := repo.PurgeResults(t.Context(), p.ResultCutoff(keyT0.Add(24*time.Hour)))
	if err != nil || n != 1 {
		t.Fatalf("PurgeResults = %d, %v; want 1", n, err)
	}
	var stored int
	if err := db.pool.QueryRow(t.Context(), "SELECT count(*) FROM quota_idempotency_keys WHERE result_output IS NOT NULL AND idempotency_key = 'old'").
		Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("old result rows = %d, %v", stored, err)
	}
	if res, err := replay(t, repo, owner, p, "new", keyT0.Add(2*time.Hour)); err != nil || res.Output != "new answer" {
		t.Fatalf("the newer result was purged: %v, %v", res, err)
	}
	if n, err := repo.PurgeResults(t.Context(), p.ResultCutoff(keyT0.Add(24*time.Hour))); err != nil || n != 0 {
		t.Fatalf("second purge = %d, %v; want 0", n, err)
	}

	db.Close()
	if _, err := repo.PurgeResults(t.Context(), keyT0); !errors.Is(err, ErrClosed) {
		t.Fatalf("PurgeResults after Close = %v, want ErrClosed", err)
	}
}

func TestGatewayResultConstraints(t *testing.T) {
	db := migratedDB(t)
	repo, owner, p := db.QuotaAccounts(), storedOwner(t, db), quotaPolicy(t, 1000)
	startAndComplete(t, repo, owner, p, "k", resID(1), "x", keyT0)

	for name, sql := range map[string]string{
		"bad hash":            "UPDATE quota_idempotency_keys SET request_hash = 'ABC' WHERE api_key_id = $1",
		"result without hash": "UPDATE quota_idempotency_keys SET request_hash = '' WHERE api_key_id = $1",
		"half a result":       "UPDATE quota_idempotency_keys SET result_input_tokens = NULL WHERE api_key_id = $1",
		"negative tokens":     "UPDATE quota_idempotency_keys SET result_output_tokens = -1 WHERE api_key_id = $1",
	} {
		if _, err := db.pool.Exec(t.Context(), sql, owner.String()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
