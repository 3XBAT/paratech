package keyauth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/cases/keyauth"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// issue stores a key generated at test time and returns its plaintext.
func issue(t *testing.T, store *testenv.KeyStore) (apikey.Credential, string) {
	t.Helper()
	idBytes, secretBytes := make([]byte, apikey.IDBytes), make([]byte, apikey.SecretBytes)
	random := testenv.Random{}
	if err := random.Fill(idBytes); err != nil {
		t.Fatal(err)
	}
	if err := random.Fill(secretBytes); err != nil {
		t.Fatal(err)
	}
	cred, err := apikey.NewCredential(idBytes, secretBytes)
	if err != nil {
		t.Fatal(err)
	}
	l, err := apikey.NewLabel("t1")
	if err != nil {
		t.Fatal(err)
	}
	k, err := apikey.New(cred, l, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	return cred, cred.Reveal()
}

func newService(t *testing.T, store *testenv.KeyStore) *keyauth.Service {
	t.Helper()
	svc, err := keyauth.New(store)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestNewRequiresFinder(t *testing.T) {
	if _, err := keyauth.New(nil); !errors.Is(err, keyauth.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestAuthenticate(t *testing.T) {
	store := testenv.NewKeyStore()
	svc := newService(t, store)
	cred, key := issue(t, store)
	revokedCred, revokedKey := issue(t, store)

	revoked, err := store.Get(t.Context(), revokedCred.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := revoked.Revoke(t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRevocation(t.Context(), revoked); err != nil {
		t.Fatal(err)
	}

	t.Run("valid key", func(t *testing.T) {
		id, err := svc.Authenticate(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		if id != cred.ID() {
			t.Fatalf("id = %s, want %s", id, cred.ID())
		}
	})

	_, other := issue(t, testenv.NewKeyStore()) // valid format, never stored here
	secretStart := len(apikey.Prefix) + 1 + 2*apikey.IDBytes + 1
	repl := byte('0')
	if key[len(key)-1] == '0' {
		repl = '1'
	}

	for _, tt := range []struct {
		name      string
		presented string
		reason    error
	}{
		{"empty", "", apikey.ErrMalformed},
		{"malformed", "pthub_nope", apikey.ErrMalformed},
		{"bearer included", "Bearer " + key, apikey.ErrMalformed},
		{"unknown id", other, apikey.ErrUnknown},
		{"wrong secret", key[:secretStart] + other[secretStart:], apikey.ErrSecretMismatch},
		{"one character changed", key[:len(key)-1] + string(repl), apikey.ErrSecretMismatch},
		{"revoked", revokedKey, apikey.ErrRevoked},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id, err := svc.Authenticate(t.Context(), tt.presented)
			if !errors.Is(err, keyauth.ErrInvalidKey) || !errors.Is(err, tt.reason) {
				t.Fatalf("err = %v, want ErrInvalidKey with %v", err, tt.reason)
			}
			if id != "" {
				t.Fatalf("id = %q on rejection", id)
			}
			if tt.presented != "" && strings.Contains(err.Error(), tt.presented[min(len(tt.presented), 6):]) {
				t.Fatal("the error quotes the presented key")
			}
		})
	}
}

func TestAuthenticateStoreFailureIsNotARejection(t *testing.T) {
	store := testenv.NewKeyStore()
	svc := newService(t, store)
	_, key := issue(t, store)

	boom := errors.New("db down")
	store.Err = boom
	_, err := svc.Authenticate(t.Context(), key)
	if !errors.Is(err, boom) || errors.Is(err, keyauth.ErrInvalidKey) {
		t.Fatalf("err = %v, want the store error and not ErrInvalidKey", err)
	}
	if errors.Is(err, shared.ErrNotFound) {
		t.Fatal("a store failure looks like an unknown key")
	}
}
