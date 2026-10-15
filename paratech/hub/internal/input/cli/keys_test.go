package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/hub/internal/cases/keys"
	"paratech/hub/internal/domain/access/apikey"
	"paratech/hub/internal/domain/shared"
	"paratech/hub/internal/input/cli"
	"paratech/hub/testenv"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fixture struct {
	cmd   *cli.Keys
	out   *bytes.Buffer
	store *testenv.KeyStore
	clock *testenv.FakeClock
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{out: &bytes.Buffer{}, store: testenv.NewKeyStore(), clock: testenv.NewFakeClock(t0)}
	svc, err := keys.New(f.store, f.clock, testenv.Random{})
	if err != nil {
		t.Fatal(err)
	}
	f.cmd, err = cli.NewKeys(svc, f.out)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// create runs Create and returns the printed key, which must be the only key-shaped line.
func (f fixture) create(t *testing.T, label string) string {
	t.Helper()
	f.out.Reset()
	if err := f.cmd.Create(t.Context(), label); err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, line := range strings.Split(f.out.String(), "\n") {
		if strings.HasPrefix(line, apikey.Prefix+"_") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("output has %d key lines, want 1:\n%s", len(found), f.out.String())
	}
	if strings.Count(f.out.String(), found[0]) != 1 {
		t.Fatal("the key is printed more than once")
	}
	return found[0]
}

func TestNewKeysRequiresDeps(t *testing.T) {
	if _, err := cli.NewKeys(nil, &bytes.Buffer{}); err == nil {
		t.Error("no error without a service")
	}
}

func TestCreatePrintsTheKeyOnce(t *testing.T) {
	f := newFixture(t)
	key := f.create(t, "tester-07")

	cred, err := apikey.ParseCredential(key)
	if err != nil {
		t.Fatalf("printed key does not parse: %v", err)
	}
	if !strings.Contains(f.out.String(), cred.ID().String()) || !strings.Contains(f.out.String(), `"tester-07"`) {
		t.Fatalf("output does not name the id and label:\n%s", f.out.String())
	}
	stored, err := f.store.Get(t.Context(), cred.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.Verify(cred); err != nil {
		t.Fatalf("printed key does not verify against the store: %v", err)
	}

	// Nothing else ever prints it again.
	f.out.Reset()
	if err := f.cmd.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	secret := key[len(apikey.Prefix)+1+2*apikey.IDBytes+1:]
	if strings.Contains(f.out.String(), secret[:16]) {
		t.Fatal("List prints the secret")
	}
}

func TestCreateRejectsABadLabel(t *testing.T) {
	f := newFixture(t)
	if err := f.cmd.Create(t.Context(), "Anna Petrova"); !errors.Is(err, apikey.ErrInvalidLabel) {
		t.Fatalf("err = %v, want ErrInvalidLabel", err)
	}
	if f.out.Len() != 0 {
		t.Fatalf("output on failure: %s", f.out.String())
	}
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	key := f.create(t, "t1")
	cred, err := apikey.ParseCredential(key)
	if err != nil {
		t.Fatal(err)
	}
	id := cred.ID().String()

	f.clock.Advance(time.Hour)
	f.out.Reset()
	if err := f.cmd.Revoke(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if want := "API key " + id + " revoked at 2026-10-15T13:00:00Z."; !strings.Contains(f.out.String(), want) {
		t.Fatalf("output %q, want %q", f.out.String(), want)
	}

	// Repeating is reported, not an error, and keeps the first time.
	f.clock.Advance(time.Hour)
	f.out.Reset()
	if err := f.cmd.Revoke(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if want := "already revoked at 2026-10-15T13:00:00Z"; !strings.Contains(f.out.String(), want) {
		t.Fatalf("output %q, want %q", f.out.String(), want)
	}

	if err := f.cmd.Revoke(t.Context(), "0123456789abcdef"); err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("unknown id: err = %v", err)
	}
	if err := f.cmd.Revoke(t.Context(), key); !errors.Is(err, shared.ErrInvalidID) || strings.Contains(err.Error(), key) {
		t.Fatalf("full key as id: err = %v, want ErrInvalidID without the key", err)
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	f.out.Reset()
	if err := f.cmd.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(f.out.String()) != "ID  LABEL  STATUS  CREATED  REVOKED" {
		t.Fatalf("empty list: %q", f.out.String())
	}

	first, _ := apikey.ParseCredential(f.create(t, "t1"))
	f.clock.Advance(time.Minute)
	f.create(t, "t2")
	if err := f.cmd.Revoke(t.Context(), first.ID().String()); err != nil {
		t.Fatal(err)
	}

	f.out.Reset()
	if err := f.cmd.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(f.out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("list:\n%s", f.out.String())
	}
	if !strings.Contains(lines[1], first.ID().String()) || !strings.Contains(lines[1], "revoked") || !strings.Contains(lines[2], "active") {
		t.Fatalf("list rows:\n%s", f.out.String())
	}

	f.store.Err = errors.New("db down")
	if err := f.cmd.List(t.Context()); err == nil {
		t.Fatal("List succeeded with a failing store")
	}
}
