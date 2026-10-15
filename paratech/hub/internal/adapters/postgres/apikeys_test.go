package postgres

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
)

var keyT0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func migratedDB(t *testing.T) *DB {
	t.Helper()
	db := openTestDB(t, testenv.PostgresURL(t))
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

// newKey builds a key from random bytes generated at test time.
func newKey(t *testing.T, label string, at time.Time) (*apikey.Key, apikey.Credential) {
	t.Helper()
	idBytes, secretBytes := make([]byte, apikey.IDBytes), make([]byte, apikey.SecretBytes)
	if err := (testenv.Random{}).Fill(idBytes); err != nil {
		t.Fatal(err)
	}
	if err := (testenv.Random{}).Fill(secretBytes); err != nil {
		t.Fatal(err)
	}
	cred, err := apikey.NewCredential(idBytes, secretBytes)
	if err != nil {
		t.Fatal(err)
	}
	l, err := apikey.NewLabel(label)
	if err != nil {
		t.Fatal(err)
	}
	k, err := apikey.New(cred, l, at)
	if err != nil {
		t.Fatal(err)
	}
	return k, cred
}

func TestAPIKeysRoundTrip(t *testing.T) {
	repo := migratedDB(t).APIKeys()
	k, cred := newKey(t, "tester-01", keyT0)

	if err := repo.Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), k.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != k.ID() || got.Label() != k.Label() || got.Status() != apikey.StatusActive ||
		!got.CreatedAt().Equal(keyT0) || !got.RevokedAt().IsZero() || !bytes.Equal(got.SecretHash(), k.SecretHash()) {
		t.Fatalf("restored key differs: %v %v %v %v", got.ID(), got.Label(), got.Status(), got.CreatedAt())
	}
	if err := got.Verify(cred); err != nil {
		t.Fatalf("restored key does not verify: %v", err)
	}

	if err := repo.Create(t.Context(), k); !errors.Is(err, shared.ErrAlreadyExists) {
		t.Fatalf("duplicate Create = %v, want ErrAlreadyExists", err)
	}
	if _, err := repo.Get(t.Context(), "0123456789abcdef"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestAPIKeysRevocation(t *testing.T) {
	repo := migratedDB(t).APIKeys()
	k, cred := newKey(t, "t1", keyT0)
	if err := repo.Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}

	if err := repo.SaveRevocation(t.Context(), k); !errors.Is(err, apikey.ErrInvalid) {
		t.Fatalf("SaveRevocation of an active key = %v, want ErrInvalid", err)
	}

	at := keyT0.Add(time.Hour)
	if err := k.Revoke(at); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRevocation(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), k.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != apikey.StatusRevoked || !got.RevokedAt().Equal(at) {
		t.Fatalf("stored revocation: %v %v", got.Status(), got.RevokedAt())
	}
	if err := got.Verify(cred); !errors.Is(err, apikey.ErrRevoked) {
		t.Fatalf("Verify after revocation = %v, want ErrRevoked", err)
	}

	// The first revocation time stays.
	later, err := apikey.Restore(k.ID(), k.Label(), k.SecretHash(), keyT0, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRevocation(t.Context(), later); !errors.Is(err, apikey.ErrAlreadyRevoked) {
		t.Fatalf("second SaveRevocation = %v, want ErrAlreadyRevoked", err)
	}
	if got, _ := repo.Get(t.Context(), k.ID()); !got.RevokedAt().Equal(at) {
		t.Fatalf("revocation time changed to %v", got.RevokedAt())
	}

	missing, _ := newKey(t, "t2", keyT0)
	if err := missing.Revoke(at); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRevocation(t.Context(), missing); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("SaveRevocation of a missing key = %v, want ErrNotFound", err)
	}
}

func TestAPIKeysConcurrentRevocationRecordsOnce(t *testing.T) {
	repo := migratedDB(t).APIKeys()
	k, _ := newKey(t, "t1", keyT0)
	if err := repo.Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}

	const n = 6
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mine, err := repo.Get(t.Context(), k.ID())
			if err == nil {
				err = mine.Revoke(keyT0.Add(time.Duration(i+1) * time.Minute))
			}
			if err == nil {
				err = repo.SaveRevocation(t.Context(), mine)
			}
			errs[i] = err
		}()
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, apikey.ErrAlreadyRevoked):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d revocations were saved, want exactly 1", won)
	}
}

func TestAPIKeysList(t *testing.T) {
	repo := migratedDB(t).APIKeys()
	if got, err := repo.List(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("empty List = %v, %v", got, err)
	}

	second, _ := newKey(t, "t2", keyT0.Add(time.Minute))
	first, _ := newKey(t, "t1", keyT0)
	for _, k := range []*apikey.Key{second, first} {
		if err := repo.Create(t.Context(), k); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID() != first.ID() || got[1].ID() != second.ID() {
		t.Fatalf("List order: %v", got)
	}
}

// TestAPIKeysStoreNoPlaintext reads every column of every row as text and checks that neither the key nor its secret
// part is there in any encoding (AC-25: only a hash is stored).
func TestAPIKeysStoreNoPlaintext(t *testing.T) {
	db := migratedDB(t)
	repo := db.APIKeys()
	k, cred := newKey(t, "t1", keyT0)
	if err := repo.Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}

	key := cred.Reveal()
	secret := key[len(apikey.Prefix)+1+2*apikey.IDBytes+1:]
	secretBytes, err := hex.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := db.pool.Query(t.Context(), "SELECT t::text, secret_hash FROM api_keys t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var text string
		var hash []byte
		if err := rows.Scan(&text, &hash); err != nil {
			t.Fatal(err)
		}
		n++
		for _, needle := range []string{key, secret, hex.EncodeToString(secretBytes)} {
			if strings.Contains(strings.ToLower(text), needle) {
				t.Fatalf("a column holds the plaintext key or secret: %s", text)
			}
		}
		if bytes.Contains(hash, secretBytes) || bytes.Contains(hash, []byte(secret)) {
			t.Fatal("secret_hash holds the secret")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}

func TestAPIKeysClosed(t *testing.T) {
	db := migratedDB(t)
	repo := db.APIKeys()
	db.Close()

	k, _ := newKey(t, "t1", keyT0)
	if err := repo.Create(t.Context(), k); !errors.Is(err, ErrClosed) {
		t.Errorf("Create = %v, want ErrClosed", err)
	}
	if _, err := repo.Get(t.Context(), k.ID()); !errors.Is(err, ErrClosed) {
		t.Errorf("Get = %v, want ErrClosed", err)
	}
	if err := repo.SaveRevocation(t.Context(), k); !errors.Is(err, ErrClosed) {
		t.Errorf("SaveRevocation = %v, want ErrClosed", err)
	}
	if _, err := repo.List(t.Context()); !errors.Is(err, ErrClosed) {
		t.Errorf("List = %v, want ErrClosed", err)
	}
}
