// Package apikey is the aggregate of a per-tester Hub API key (PRD section 12, AC-25): the key id, the label, the
// hash of the secret and the revocation state. The plaintext secret is never part of the aggregate.
package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"time"

	"paratech/hub/internal/domain/shared"
)

// Status is the state of a key.
type Status string

// Statuses. The only transition is active -> revoked (see canMove); a revoked key stays revoked.
const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

// canMove is the transition table of a key (ARCH-19).
func canMove(from, to Status) bool {
	switch from {
	case StatusActive:
		return to == StatusRevoked
	default:
		return false
	}
}

// Key is the aggregate root. Fields are unexported; build it with New or Restore (ARCH-14).
type Key struct {
	id        shared.APIKeyID
	label     Label
	hash      [sha256.Size]byte
	status    Status
	createdAt time.Time
	revokedAt time.Time
}

// New creates an active key from a freshly issued credential. Only the hash of the secret is kept.
func New(c Credential, label Label, now time.Time) (*Key, error) {
	if err := c.id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len(c.secret) != secretLen {
		return nil, fmt.Errorf("%w: credential has no secret", ErrInvalid)
	}
	if label.IsZero() {
		return nil, fmt.Errorf("%w: label is required", ErrInvalid)
	}
	if now.IsZero() {
		return nil, fmt.Errorf("%w: creation time is required", ErrInvalid)
	}
	return &Key{id: c.id, label: label, hash: c.hash(), status: StatusActive, createdAt: now}, nil
}

// Restore rebuilds a stored key without side effects. A zero revokedAt means the key is active.
func Restore(id shared.APIKeyID, label Label, secretHash []byte, createdAt, revokedAt time.Time) (*Key, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if label.IsZero() {
		return nil, fmt.Errorf("%w: label is required", ErrInvalid)
	}
	if len(secretHash) != sha256.Size {
		return nil, fmt.Errorf("%w: secret hash must be %d bytes", ErrInvalid, sha256.Size)
	}
	if createdAt.IsZero() {
		return nil, fmt.Errorf("%w: creation time is required", ErrInvalid)
	}

	k := &Key{id: id, label: label, status: StatusActive, createdAt: createdAt}
	copy(k.hash[:], secretHash)
	if !revokedAt.IsZero() {
		k.status, k.revokedAt = StatusRevoked, revokedAt
	}
	return k, nil
}

// ID returns the public key id.
func (k *Key) ID() shared.APIKeyID { return k.id }

// Label returns the label of the key.
func (k *Key) Label() Label { return k.label }

// Status returns the state of the key.
func (k *Key) Status() Status { return k.status }

// CreatedAt returns when the key was issued.
func (k *Key) CreatedAt() time.Time { return k.createdAt }

// RevokedAt returns when the key was revoked, or the zero time for an active key.
func (k *Key) RevokedAt() time.Time { return k.revokedAt }

// SecretHash returns a copy of the stored hash, for the repository.
func (k *Key) SecretHash() []byte {
	h := k.hash
	return h[:]
}

// Revoke makes the key unusable from now on. A revocation must never fail because of the clock, so a time before
// the creation (clock skew) is recorded as the creation time.
func (k *Key) Revoke(now time.Time) error {
	if !canMove(k.status, StatusRevoked) {
		return ErrAlreadyRevoked
	}
	if now.IsZero() {
		return fmt.Errorf("%w: revocation time is required", ErrInvalid)
	}
	if now.Before(k.createdAt) {
		now = k.createdAt
	}
	k.status, k.revokedAt = StatusRevoked, now
	return nil
}

// Verify checks a presented credential against the key. The hash comparison is constant time, and it runs before
// the revocation check so that a revoked key costs the same as an active one.
func (k *Key) Verify(c Credential) error {
	h := c.hash()
	if subtle.ConstantTimeCompare(h[:], k.hash[:]) != 1 || c.id != k.id {
		return ErrSecretMismatch
	}
	if k.status == StatusRevoked {
		return ErrRevoked
	}
	return nil
}

// VerifyMissing is Verify for a key that does not exist: it does the same hashing and comparison work against a
// dummy hash and always returns ErrUnknown, so an unknown id is not cheaper to check than a wrong secret.
func VerifyMissing(c Credential) error {
	var dummy [sha256.Size]byte
	h := c.hash()
	_ = subtle.ConstantTimeCompare(h[:], dummy[:]) // only the cost matters, not the result
	return ErrUnknown
}
