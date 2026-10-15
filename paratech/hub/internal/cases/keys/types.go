package keys

import (
	"fmt"
	"log/slog"
	"time"

	"paratech/hub/internal/domain/access/apikey"
)

// KeyInfo describes a stored key. It never carries the secret or its hash.
type KeyInfo struct {
	ID        string
	Label     string
	Revoked   bool
	CreatedAt time.Time
	RevokedAt time.Time // zero while the key is active
}

// Issued is the result of Issue: the new key's description and its plaintext key, which is never stored or shown again.
type Issued struct {
	KeyInfo
	Key string
}

// String hides the plaintext key from fmt.
func (i Issued) String() string {
	return fmt.Sprintf("{ID:%s Label:%s Key:(redacted)}", i.ID, i.Label)
}

// GoString hides the plaintext key from %#v.
func (i Issued) GoString() string { return i.String() }

// LogValue hides the plaintext key from slog, whose JSON handler ignores String.
func (i Issued) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", i.ID), slog.String("label", i.Label))
}

func info(k *apikey.Key) KeyInfo {
	return KeyInfo{
		ID:        k.ID().String(),
		Label:     k.Label().String(),
		Revoked:   k.Status() == apikey.StatusRevoked,
		CreatedAt: k.CreatedAt(),
		RevokedAt: k.RevokedAt(),
	}
}
