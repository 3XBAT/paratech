package tests

// Contract of the secret stores (TASK-037, PRD section 12, AC-19, ARCH-34). One table runs against
// testenv.MemCredentials everywhere and against the real wincred adapter on Windows, so the fake cannot drift
// from the adapter it replaces.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/wincred"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// secretReader is what the reading adapters (smtp, hubclient, Google OAuth) will declare for themselves.
type secretReader interface {
	Get(ctx context.Context, name string) (string, error)
}

// Both stores satisfy every CredentialStore port and the reader shape; a port change breaks the build here.
var (
	_ senddelivery.CredentialStore = (*wincred.Store)(nil)
	_ senddelivery.CredentialStore = (*testenv.MemCredentials)(nil)
	_ secretReader                 = (*wincred.Store)(nil)
	_ secretReader                 = (*testenv.MemCredentials)(nil)
)

// secretStore is the full method set under contract.
type secretStore interface {
	senddelivery.CredentialStore
	secretReader
}

func TestCredentialStoreContract(t *testing.T) {
	stores := map[string]func(t *testing.T) secretStore{
		"mem": func(*testing.T) secretStore { return testenv.NewMemCredentials() },
	}
	if runtime.GOOS == "windows" {
		stores["wincred"] = realWincred
	}

	for name, open := range stores {
		t.Run(name, func(t *testing.T) {
			runCredentialContract(t, open(t))
		})
	}
}

// realWincred opens the real Credential Manager under a unique test service and deletes its entries afterwards.
func realWincred(t *testing.T) secretStore {
	t.Helper()
	service := "contract-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	s, err := wincred.New(service)
	if err != nil {
		t.Fatal(err)
	}
	// Only a session without a credential set (service or network logon) may skip; any other error fails.
	_, err = s.Exists(context.Background(), contractNames[0])
	switch credmanVerdict(err, os.Getenv) {
	case credmanSkip:
		t.Skipf("no credential set in this logon session (set %s=1 to fail): %v", requireCredmanEnv, err)
	case credmanFail:
		t.Fatalf("credential manager: %v", err)
	}

	t.Cleanup(func() {
		for _, n := range contractNames {
			if err := s.Delete(context.Background(), n); err != nil {
				t.Errorf("cleanup Delete(%s): %v", n, err)
			}
		}
	})
	return s
}

// requireCredmanEnv=1 turns a session without a credential set into a failure instead of a skip.
const requireCredmanEnv = "PARATECH_REQUIRE_CREDMAN"

type credmanStatus int

const (
	credmanRun credmanStatus = iota
	credmanSkip
	credmanFail
)

// credmanVerdict decides what the real-wincred subtest does with the error of its probe.
func credmanVerdict(err error, getenv func(string) string) credmanStatus {
	switch {
	case err == nil:
		return credmanRun
	case errors.Is(err, wincred.ErrNoSession) && getenv(requireCredmanEnv) != "1":
		return credmanSkip
	}
	return credmanFail
}

func TestCredmanVerdict(t *testing.T) {
	noEnv := func(string) string { return "" }
	force := func(k string) string {
		if k == requireCredmanEnv {
			return "1"
		}
		return ""
	}
	noSession := fmt.Errorf("credential manager read: %w", wincred.ErrNoSession)

	cases := []struct {
		desc   string
		err    error
		getenv func(string) string
		want   credmanStatus
	}{
		{"usable", nil, noEnv, credmanRun},
		{"no session", noSession, noEnv, credmanSkip},
		{"no session, forced", noSession, force, credmanFail},
		{"other storage error", fmt.Errorf("%w: x", shared.ErrStorage), noEnv, credmanFail},
		{"unsupported", wincred.ErrUnsupported, noEnv, credmanFail},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := credmanVerdict(tc.err, tc.getenv); got != tc.want {
				t.Fatalf("credmanVerdict = %d, want %d", got, tc.want)
			}
		})
	}
}

// Boundary names and secrets of the contract.
var (
	name64 = strings.Repeat("n", wincred.MaxNameLen)
	name65 = name64 + "n"
)

// contractNames are every name the contract writes, for cleanup.
var contractNames = []string{"smtp-password", "limit", "hub.api_key", name64}

func runCredentialContract(t *testing.T, s secretStore) {
	ctx := context.Background()
	limit := strings.Repeat("k", wincred.MaxSecretBytes)
	set := func(name, secret string) func() error {
		return func() error { return s.Set(ctx, name, secret) }
	}

	steps := []struct {
		desc string
		run  func() error
		want error
	}{
		{"missing secret is not found", func() error { _, err := s.Get(ctx, "smtp-password"); return err }, shared.ErrNotFound},
		{"set", func() error { return s.Set(ctx, "smtp-password", "first") }, nil},
		{"get returns it", expectGet(ctx, s, "smtp-password", "first"), nil},
		{"overwrite", func() error { return s.Set(ctx, "smtp-password", "second") }, nil},
		{"get returns the new value", expectGet(ctx, s, "smtp-password", "second"), nil},
		{"exists", expectExists(ctx, s, "smtp-password", true), nil},
		{"value at the size limit", func() error { return s.Set(ctx, "limit", limit) }, nil},
		{"get at the size limit", expectGet(ctx, s, "limit", limit), nil},
		{"one byte over is rejected", set("limit", limit+"k"), shared.ErrInvalidSecret},
		{"rejected value kept the old one", expectGet(ctx, s, "limit", limit), nil},
		{"two-byte runes at the size limit", set("limit", strings.Repeat("я", wincred.MaxSecretBytes/2)), nil},
		{"get two-byte runes", expectGet(ctx, s, "limit", strings.Repeat("я", wincred.MaxSecretBytes/2)), nil},
		{"two-byte runes one byte over", set("limit", strings.Repeat("я", wincred.MaxSecretBytes/2)+"k"), shared.ErrInvalidSecret},
		{"limit in runes is over in bytes", set("limit", strings.Repeat("я", wincred.MaxSecretBytes)), shared.ErrInvalidSecret},
		{"empty secret is rejected", set("hub.api_key", ""), shared.ErrInvalidSecret},

		{"name at the length limit", set(name64, "x"), nil},
		{"get at the name limit", expectGet(ctx, s, name64, "x"), nil},
		{"delete at the name limit", func() error { return s.Delete(ctx, name64) }, nil},
		{"name one over the limit", set(name65, "x"), shared.ErrInvalidID},
		{"get one over the name limit", func() error { _, err := s.Get(ctx, name65); return err }, shared.ErrInvalidID},
		{"empty name", set("", "x"), shared.ErrInvalidID},
		{"leading dot", set(".key", "x"), shared.ErrInvalidID},
		{"leading dash", set("-key", "x"), shared.ErrInvalidID},
		{"leading underscore", set("_key", "x"), shared.ErrInvalidID},
		{"inner dot, dash and underscore", set("hub.api_key", "x"), nil},
		{"space inside", set("smtp password", "x"), shared.ErrInvalidID},
		{"leading space", set(" key", "x"), shared.ErrInvalidID},
		{"non-ASCII name", set("пароль", "x"), shared.ErrInvalidID},
		{"non-ASCII letter inside", set("naïve", "x"), shared.ErrInvalidID},
		{"path-like name", set("../hub", "x"), shared.ErrInvalidID},
		{"backslash", set(`a\b`, "x"), shared.ErrInvalidID},
		{"control character", set("a\nb", "x"), shared.ErrInvalidID},
		{"upper case", func() error { _, err := s.Exists(ctx, "Smtp"); return err }, shared.ErrInvalidID},
		{"delete", func() error { return s.Delete(ctx, "smtp-password") }, nil},
		{"get after delete", func() error { _, err := s.Get(ctx, "smtp-password"); return err }, shared.ErrNotFound},
		{"exists after delete", expectExists(ctx, s, "smtp-password", false), nil},
		{"delete is idempotent", func() error { return s.Delete(ctx, "smtp-password") }, nil},
	}
	for _, st := range steps {
		err := st.run()
		if (st.want == nil) != (err == nil) || !errors.Is(err, st.want) {
			t.Fatalf("%s: err = %v, want %v", st.desc, err, st.want)
		}
		if err != nil && strings.Contains(err.Error(), "first") {
			t.Fatalf("%s: error reveals a secret: %v", st.desc, err)
		}
	}
}

func expectGet(ctx context.Context, s secretStore, name, want string) func() error {
	return func() error {
		got, err := s.Get(ctx, name)
		if err != nil {
			return err
		}
		if got != want {
			return errors.New("Get returned another value (" + strconv.Itoa(len(got)) + " bytes)")
		}
		return nil
	}
}

func expectExists(ctx context.Context, s secretStore, name string, want bool) func() error {
	return func() error {
		got, err := s.Exists(ctx, name)
		if err != nil {
			return err
		}
		if got != want {
			return errors.New("Exists = " + strconv.FormatBool(got))
		}
		return nil
	}
}
