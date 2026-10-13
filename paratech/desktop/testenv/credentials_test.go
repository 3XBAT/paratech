package testenv

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

// The semantics shared with wincred are covered by tests/credentialstore_contract_test.go; these are fake-only.

func TestMemCredentialsFail(t *testing.T) {
	ctx := context.Background()
	m := NewMemCredentials()
	if err := m.Set(ctx, "key", "v"); err != nil {
		t.Fatal(err)
	}

	m.Fail(shared.ErrStorageBusy)
	calls := map[string]func() error{
		"set":    func() error { return m.Set(ctx, "key", "w") },
		"get":    func() error { _, err := m.Get(ctx, "key"); return err },
		"exists": func() error { _, err := m.Exists(ctx, "key"); return err },
		"delete": func() error { return m.Delete(ctx, "key") },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, shared.ErrStorageBusy) {
			t.Errorf("%s = %v, want the injected error", name, err)
		}
	}
	if err := m.Set(ctx, "Bad", "w"); !errors.Is(err, shared.ErrInvalidID) {
		t.Errorf("validation runs before the injected failure: %v", err)
	}

	m.Fail(nil)
	if got, err := m.Get(ctx, "key"); err != nil || got != "v" {
		t.Fatalf("after healing Get = %q, %v; want the old value", got, err)
	}
}

func TestMemCredentialsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewMemCredentials().Set(ctx, "key", "v"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Set = %v, want context.Canceled", err)
	}
}

func TestMemCredentialsRedacts(t *testing.T) {
	const secret = "sk-very-secret-value"
	m := NewMemCredentials()
	if err := m.Set(context.Background(), "key", secret); err != nil {
		t.Fatal(err)
	}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(verb, m); strings.Contains(out, secret) {
			t.Errorf("%s reveals the secret: %s", verb, out)
		}
	}
	if err := m.Set(context.Background(), "key", secret+strings.Repeat("x", memMaxSecretBytes)); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("oversized Set error = %v; want an error without the secret", err)
	}
}

func TestMemCredentialsConcurrent(t *testing.T) {
	ctx := context.Background()
	m := NewMemCredentials()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "k" + strconv.Itoa(i)
			for range 50 {
				_ = m.Set(ctx, name, "v")
				_, _ = m.Get(ctx, name)
				_, _ = m.Exists(ctx, name)
				_ = m.Delete(ctx, name)
			}
		}()
	}
	wg.Wait()
	if m.Len() != 0 {
		t.Fatalf("Len = %d, want 0", m.Len())
	}
}
