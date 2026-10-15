package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"paratech/hub/internal/domain/shared"
)

// Key format: "pthub_<id>_<secret>", id 8 random bytes and secret 32 random bytes, both lowercase hex. The prefix
// makes a leaked key recognizable to secret scanners without looking like a provider key (sk-, ghp_, AKIA).
const (
	// Prefix starts every issued key.
	Prefix = "pthub"

	// IDBytes and SecretBytes are the numbers of random bytes of the id and the secret.
	IDBytes     = 8
	SecretBytes = 32

	separator = '_'
	idLen     = 2 * IDBytes
	secretLen = 2 * SecretBytes

	// KeyLen is the exact length of an issued key.
	KeyLen = len(Prefix) + 1 + idLen + 1 + secretLen
)

// Credential is an API key as text: the public id and the secret. It is either newly issued (NewCredential) or
// presented by a client (ParseCredential). fmt and slog print it redacted; only Reveal returns the key text.
type Credential struct {
	id     shared.APIKeyID
	secret string
}

// NewCredential builds a new key from random bytes supplied by the caller (randomness stays outside the domain,
// ARCH-15). All-zero material is rejected as a sign of a broken random source.
func NewCredential(idBytes, secretBytes []byte) (Credential, error) {
	if len(idBytes) != IDBytes || len(secretBytes) != SecretBytes {
		return Credential{}, fmt.Errorf("%w: want %d id bytes and %d secret bytes", ErrInvalidMaterial, IDBytes, SecretBytes)
	}
	if allZero(idBytes) || allZero(secretBytes) {
		return Credential{}, fmt.Errorf("%w: all-zero bytes", ErrInvalidMaterial)
	}
	return Credential{id: shared.APIKeyID(hex.EncodeToString(idBytes)), secret: hex.EncodeToString(secretBytes)}, nil
}

// ParseCredential checks the exact format of a presented key before any storage access: length, prefix, separators
// and the lowercase hex charset. The error never contains the input.
func ParseCredential(raw string) (Credential, error) {
	if len(raw) != KeyLen {
		return Credential{}, ErrMalformed
	}
	rest, ok := strings.CutPrefix(raw, Prefix+string(separator))
	if !ok {
		return Credential{}, ErrMalformed
	}
	id, secret := rest[:idLen], rest[idLen+1:]
	if rest[idLen] != separator || shared.APIKeyID(id).Validate() != nil || !isLowerHex(secret) {
		return Credential{}, ErrMalformed
	}
	return Credential{id: shared.APIKeyID(id), secret: secret}, nil
}

// ID returns the public part of the key.
func (c Credential) ID() shared.APIKeyID { return c.id }

// Reveal returns the full key text. Only the issuing use case calls it, to show the key to the operator once.
func (c Credential) Reveal() string {
	if c.id == "" {
		return ""
	}
	return Prefix + string(separator) + string(c.id) + string(separator) + c.secret
}

// String hides the secret from fmt and from slog text output.
func (c Credential) String() string { return "apikey.Credential(redacted)" }

// GoString hides the secret from %#v.
func (c Credential) GoString() string { return c.String() }

// hash is what the Hub stores instead of the secret. A plain SHA-256 is enough: the secret is 256 random bits, so
// there is nothing to brute-force that a slow password hash would protect.
func (c Credential) hash() [sha256.Size]byte {
	return sha256.Sum256([]byte(c.secret))
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// isLowerHex reports whether s is non-empty and consists only of 0-9 and a-f.
func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
