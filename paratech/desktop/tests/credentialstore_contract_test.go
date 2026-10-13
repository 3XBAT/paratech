package tests

// Contract of the secret stores (TASK-037, PRD section 12, AC-19, ARCH-34). One table runs against
// testenv.MemCredentials everywhere and against the real wincred adapter on Windows, so the fake cannot drift
// from the adapter it replaces.

import (
	"context"
	"errors"
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
	// A session without a credential set (service or network logon) cannot run the contract; the adapter tests
	// report that case themselves.
	if _, err := s.Exists(context.Background(), contractNames[0]); err != nil {
		t.Skipf("credential manager not usable in this session: %v", err)
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

// contractNames are every name the contract writes, for cleanup.
var contractNames = []string{"smtp-password", "limit", "hub.api_key"}

func runCredentialContract(t *testing.T, s secretStore) {
	ctx := context.Background()
	limit := strings.Repeat("k", wincred.MaxSecretBytes)

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
		{"one byte over is rejected", func() error { return s.Set(ctx, "limit", limit+"k") }, shared.ErrInvalidSecret},
		{"rejected value kept the old one", expectGet(ctx, s, "limit", limit), nil},
		{"empty secret is rejected", func() error { return s.Set(ctx, "hub.api_key", "") }, shared.ErrInvalidSecret},
		{"path-like name", func() error { return s.Set(ctx, "../hub", "x") }, shared.ErrInvalidID},
		{"control character", func() error { return s.Set(ctx, "a\nb", "x") }, shared.ErrInvalidID},
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
