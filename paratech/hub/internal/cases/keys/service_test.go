package keys_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/testenv"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc   *keys.Service
	store *testenv.KeyStore
	clock *testenv.FakeClock
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{store: testenv.NewKeyStore(), clock: testenv.NewFakeClock(t0)}
	svc, err := keys.New(f.store, f.clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func TestNewRequiresPorts(t *testing.T) {
	store, clock := testenv.NewKeyStore(), testenv.NewFakeClock(t0)
	for name, err := range map[string]error{
		"repo":   second(keys.New(nil, clock, testenv.Random{})),
		"clock":  second(keys.New(store, nil, testenv.Random{})),
		"random": second(keys.New(store, clock, nil)),
	} {
		if !errors.Is(err, keys.ErrInvalidConfig) {
			t.Errorf("nil %s: err = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func second[T any](_ T, err error) error { return err }

func TestIssue(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.Issue(t.Context(), "tester-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "tester-01" || got.Revoked || !got.CreatedAt.Equal(t0) || !got.RevokedAt.IsZero() {
		t.Fatalf("unexpected info: %+v", got.KeyInfo)
	}

	cred, err := apikey.ParseCredential(got.Key)
	if err != nil {
		t.Fatalf("issued key does not parse: %v", err)
	}
	if cred.ID().String() != got.ID {
		t.Fatalf("key id %s, info id %s", cred.ID(), got.ID)
	}

	// The store holds the SHA-256 of the secret and nothing that contains the key or the secret.
	secret := got.Key[len(apikey.Prefix)+1+len(got.ID)+1:]
	hashes := f.store.Hashes()
	want := sha256.Sum256([]byte(secret))
	if len(hashes) != 1 || !bytes.Equal(hashes[0], want[:]) {
		t.Fatal("the store does not hold exactly the hash of the secret")
	}
	stored, err := f.store.Get(t.Context(), cred.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.Verify(cred); err != nil {
		t.Fatalf("stored key does not verify the issued one: %v", err)
	}

	// fmt and slog never print the plaintext key.
	for _, out := range []string{fmt.Sprint(got), fmt.Sprintf("%+v", got), fmt.Sprintf("%#v", got)} {
		if strings.Contains(out, secret) {
			t.Errorf("Issued prints the secret: %s", out)
		}
	}
}

func TestIssueGivesDistinctKeys(t *testing.T) {
	f := newFixture(t)
	seen := map[string]bool{}
	for range 20 {
		got, err := f.svc.Issue(t.Context(), "tester-01")
		if err != nil {
			t.Fatal(err)
		}
		if seen[got.ID] || seen[got.Key] {
			t.Fatal("a key or id was issued twice")
		}
		seen[got.ID], seen[got.Key] = true, true
	}
}

func TestIssueFailures(t *testing.T) {
	t.Run("invalid label", func(t *testing.T) {
		f := newFixture(t)
		for _, l := range []string{"", "Anna Petrova", "anna@example.com", strings.Repeat("a", apikey.MaxLabelLen+1)} {
			if _, err := f.svc.Issue(t.Context(), l); !errors.Is(err, apikey.ErrInvalidLabel) {
				t.Errorf("label %q: err = %v, want ErrInvalidLabel", l, err)
			}
		}
		if n := len(f.store.Hashes()); n != 0 {
			t.Fatalf("%d keys stored after failures", n)
		}
	})

	t.Run("random source fails", func(t *testing.T) {
		boom := errors.New("no entropy")
		svc, err := keys.New(testenv.NewKeyStore(), testenv.NewFakeClock(t0), testenv.Random{Err: boom})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Issue(t.Context(), "t1"); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the random error", err)
		}
	})

	t.Run("broken random source", func(t *testing.T) {
		svc, err := keys.New(testenv.NewKeyStore(), testenv.NewFakeClock(t0), testenv.ZeroRandom{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Issue(t.Context(), "t1"); !errors.Is(err, apikey.ErrInvalidMaterial) {
			t.Fatalf("err = %v, want ErrInvalidMaterial", err)
		}
	})

	t.Run("store fails", func(t *testing.T) {
		f := newFixture(t)
		boom := errors.New("db down")
		f.store.Err = boom
		got, err := f.svc.Issue(t.Context(), "t1")
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the store error", err)
		}
		if got.Key != "" {
			t.Fatal("a key was returned although it was not stored")
		}
	})
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	issued, err := f.svc.Issue(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}

	f.clock.Advance(time.Hour)
	got, err := f.svc.Revoke(t.Context(), issued.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := t0.Add(time.Hour)
	if !got.Revoked || !got.RevokedAt.Equal(at) {
		t.Fatalf("after Revoke: %+v", got)
	}

	cred, err := apikey.ParseCredential(issued.Key)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.store.Get(t.Context(), cred.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.Verify(cred); !errors.Is(err, apikey.ErrRevoked) {
		t.Fatalf("stored key after Revoke: %v, want ErrRevoked", err)
	}

	// A second revocation reports the first time and leaves the store unchanged.
	f.clock.Advance(time.Hour)
	again, err := f.svc.Revoke(t.Context(), issued.ID)
	if !errors.Is(err, apikey.ErrAlreadyRevoked) {
		t.Fatalf("second Revoke: %v, want ErrAlreadyRevoked", err)
	}
	if !again.Revoked || !again.RevokedAt.Equal(at) {
		t.Fatalf("second Revoke reports %+v, want the first time", again)
	}
}

func TestRevokeFailures(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Revoke(t.Context(), "not-an-id"); !errors.Is(err, shared.ErrInvalidID) {
		t.Errorf("invalid id: err = %v, want ErrInvalidID", err)
	}
	if _, err := f.svc.Revoke(t.Context(), "0123456789abcdef"); !errors.Is(err, shared.ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestConcurrentRevokeRecordsOnce(t *testing.T) {
	f := newFixture(t)
	issued, err := f.svc.Issue(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Revoke(context.WithoutCancel(t.Context()), issued.ID)
		}()
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, apikey.ErrAlreadyRevoked):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d revocations succeeded, want exactly 1", ok)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	if got, err := f.svc.List(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v, %v", got, err)
	}

	first, err := f.svc.Issue(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	second, err := f.svc.Issue(t.Context(), "t2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Revoke(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("List = %+v, want the two keys oldest first", got)
	}
	if !got[0].Revoked || got[1].Revoked {
		t.Fatalf("revocation state: %+v", got)
	}
	for _, k := range got {
		if out := fmt.Sprintf("%+v", k); strings.Contains(out, first.Key[len(first.Key)-16:]) || strings.Contains(out, second.Key[len(second.Key)-16:]) {
			t.Fatalf("List exposes key material: %s", out)
		}
	}

	f.store.Err = errors.New("db down")
	if _, err := f.svc.List(t.Context()); err == nil {
		t.Fatal("List succeeded with a failing store")
	}
}
