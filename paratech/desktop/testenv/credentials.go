package testenv

import (
	"context"
	"fmt"
	"sync"

	"paratech/desktop/internal/domain/shared"
)

// Mirrors of the wincred limits (adapter tests import testenv, so testenv code does not import adapters).
// tests/credentialstore_contract_test.go runs one contract against both stores; credentials_test.go compares the
// limits and the name rule with wincred directly.
const (
	memMaxSecretBytes = 5 * 512
	memMaxNameLen     = 64
)

// MemCredentials is an in-memory secret store with the semantics of the wincred adapter: Get, Set, Exists and
// Delete, the same name rules and size limit, shared.ErrNotFound for a missing secret, idempotent Delete.
// Formatting it never prints the secrets.
type MemCredentials struct {
	mu      sync.Mutex
	secrets map[string]string
	failErr error
}

// NewMemCredentials returns an empty store.
func NewMemCredentials() *MemCredentials { return &MemCredentials{secrets: map[string]string{}} }

// Fail makes every later call fail with err (for example shared.ErrStorage); nil heals the store.
func (m *MemCredentials) Fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failErr = err
}

// Set stores or replaces the secret under name.
func (m *MemCredentials) Set(ctx context.Context, name, secret string) error {
	if err := m.check(ctx, name); err != nil {
		return err
	}
	switch {
	case secret == "":
		return fmt.Errorf("%w: empty", shared.ErrInvalidSecret)
	case len(secret) > memMaxSecretBytes:
		return fmt.Errorf("%w: larger than %d bytes", shared.ErrInvalidSecret, memMaxSecretBytes)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.secrets[name] = secret
	return nil
}

// Get returns the secret under name, or an error wrapping shared.ErrNotFound.
func (m *MemCredentials) Get(ctx context.Context, name string) (string, error) {
	if err := m.check(ctx, name); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return "", m.failErr
	}
	v, ok := m.secrets[name]
	if !ok {
		return "", fmt.Errorf("%w: credential", shared.ErrNotFound)
	}
	return v, nil
}

// Exists reports whether a secret is stored under name.
func (m *MemCredentials) Exists(ctx context.Context, name string) (bool, error) {
	if err := m.check(ctx, name); err != nil {
		return false, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return false, m.failErr
	}
	_, ok := m.secrets[name]
	return ok, nil
}

// Delete removes the secret under name; a missing secret is not an error.
func (m *MemCredentials) Delete(ctx context.Context, name string) error {
	if err := m.check(ctx, name); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	delete(m.secrets, name)
	return nil
}

// Len returns the number of stored secrets.
func (m *MemCredentials) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.secrets)
}

// String redacts the content for %v and %s.
func (m *MemCredentials) String() string {
	return fmt.Sprintf("testenv.MemCredentials{%d secrets}", m.Len())
}

// GoString redacts the content for %#v.
func (m *MemCredentials) GoString() string { return m.String() }

// check applies the context and the wincred name rule; the injected failure comes after validation, like a
// backend failure in the adapter.
func (m *MemCredentials) check(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !memValidName(name) {
		return fmt.Errorf("%w: credential name", shared.ErrInvalidID)
	}
	return nil
}

// memValidName is the wincred rule: 1..64 of [a-z0-9._-], starting with a letter or digit.
func memValidName(s string) bool {
	if s == "" || len(s) > memMaxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '.' && c != '_' && c != '-') {
			return false
		}
	}
	return true
}
