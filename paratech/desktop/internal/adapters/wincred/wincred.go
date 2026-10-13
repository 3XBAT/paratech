// Package wincred keeps secrets (mail credentials, the Hub API key, Google refresh tokens) in Windows Credential
// Manager (PRD sections 4.3 and 12, AC-19, ARCH-34). Secrets never go to SQLite, files or logs (hard rule 4).
//
// A Store serves one service and writes generic credentials named "paratech/<service>/<name>". Its method set is
// the union of the consumer ports: Set, Exists and Delete satisfy senddelivery.CredentialStore; Get is for the
// adapters that read their own secret (smtp, hubclient, Google OAuth), which declare their own narrow interface.
//
// Off Windows every call fails with ErrUnsupported, so a production build on another OS fails loudly instead of
// keeping secrets somewhere weaker. Tests use testenv.MemCredentials.
package wincred

import (
	"context"
	"errors"
	"fmt"

	"paratech/desktop/internal/domain/shared"
)

const (
	// TargetPrefix starts every target name written by the app.
	TargetPrefix = "paratech/"

	// MaxSecretBytes is CRED_MAX_CREDENTIAL_BLOB_SIZE (5*512); longer secrets are rejected, never truncated.
	MaxSecretBytes = 5 * 512

	// MaxServiceLen and MaxNameLen bound the identifiers; target names stay far below the 32767-char API limit.
	MaxServiceLen = 32
	MaxNameLen    = 64
)

var (
	// ErrUnsupported means Credential Manager is not available (any OS but Windows).
	ErrUnsupported = fmt.Errorf("wincred: credential manager is available only on Windows: %w", errors.ErrUnsupported)

	// ErrInvalidName means a service or credential name is not a fixed identifier of the app.
	ErrInvalidName = fmt.Errorf("%w: credential name", shared.ErrInvalidID)

	// ErrSecretTooLarge means the secret exceeds MaxSecretBytes.
	ErrSecretTooLarge = fmt.Errorf("%w: larger than %d bytes", shared.ErrInvalidSecret, MaxSecretBytes)

	// ErrEmptySecret means Set got an empty secret; remove a secret with Delete instead.
	ErrEmptySecret = fmt.Errorf("%w: empty", shared.ErrInvalidSecret)
)

// backend is the OS credential API. Errors are already mapped: shared.ErrNotFound, storage errors or ErrUnsupported.
type backend interface {
	write(target string, blob []byte) error
	read(target string) ([]byte, error)
	remove(target string) error
}

// Store keeps the secrets of one service. It holds no secret itself. Create it with New.
type Store struct {
	prefix string
	sys    backend
}

// New returns the store of service, for example "mail", "hub" or "google".
func New(service string) (*Store, error) {
	return newStore(service, newBackend())
}

func newStore(service string, sys backend) (*Store, error) {
	if !validIdent(service, MaxServiceLen) {
		return nil, fmt.Errorf("%w: service", ErrInvalidName)
	}
	return &Store{prefix: TargetPrefix + service + "/", sys: sys}, nil
}

// Set stores or replaces the secret under name.
func (s *Store) Set(ctx context.Context, name, secret string) error {
	target, err := s.target(ctx, name)
	if err != nil {
		return err
	}
	if err := CheckSecret(secret); err != nil {
		return err
	}

	blob := []byte(secret)
	defer clear(blob)
	return s.sys.write(target, blob)
}

// Get returns the secret under name, or an error wrapping shared.ErrNotFound. The returned string cannot be wiped;
// callers keep it only as long as needed.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	target, err := s.target(ctx, name)
	if err != nil {
		return "", err
	}

	blob, err := s.sys.read(target)
	if err != nil {
		return "", err
	}
	defer clear(blob)
	return string(blob), nil
}

// Exists reports whether a secret is stored under name, without returning it.
func (s *Store) Exists(ctx context.Context, name string) (bool, error) {
	target, err := s.target(ctx, name)
	if err != nil {
		return false, err
	}

	blob, err := s.sys.read(target)
	clear(blob)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, shared.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

// Delete removes the secret under name; a missing secret is not an error.
func (s *Store) Delete(ctx context.Context, name string) error {
	target, err := s.target(ctx, name)
	if err != nil {
		return err
	}

	if err := s.sys.remove(target); err != nil && !errors.Is(err, shared.ErrNotFound) {
		return err
	}
	return nil
}

// target checks the context and the name and returns the full target name.
func (s *Store) target(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := CheckName(name); err != nil {
		return "", err
	}
	return s.prefix + name, nil
}

// CheckName reports whether name can be a credential name: 1..MaxNameLen of [a-z0-9._-], starting with a letter
// or digit. No separators, wildcards, spaces or control characters. Lowercase only: target names are
// case-insensitive in Credential Manager, so "Key" and "key" would silently share one entry.
func CheckName(name string) error {
	if !validIdent(name, MaxNameLen) {
		return ErrInvalidName
	}
	return nil
}

// CheckSecret reports whether secret can be stored: non-empty and at most MaxSecretBytes bytes of UTF-8.
func CheckSecret(secret string) error {
	switch {
	case secret == "":
		return ErrEmptySecret
	case len(secret) > MaxSecretBytes:
		return ErrSecretTooLarge
	}
	return nil
}

// validIdent is the shared rule of service and credential names.
func validIdent(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen || !isLowerAlnum(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isLowerAlnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isLowerAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}
