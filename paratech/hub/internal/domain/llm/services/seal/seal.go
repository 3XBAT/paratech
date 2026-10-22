// Package seal signs the tool calls an AgentStep returns, so a client can echo them in later steps but cannot forge
// or alter them (ADR-0023). A seal is a key version byte followed by HMAC-SHA256 over a canonical, length-prefixed
// encoding of the caller's API key ID, the thread ID, the profile, the catalog version and every call in order.
//
// The secrets come from the Hub secret configuration and are passed in; the package has no I/O, time or randomness
// (ARCH-15).
package seal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"strconv"

	"paratech/hub/internal/domain/llm/values"
	"paratech/hub/internal/domain/shared"
)

// MinSecretBytes is the least length of a seal secret.
const MinSecretBytes = 32

// Size is the length of a seal: the key version byte and the HMAC-SHA256.
const Size = 1 + sha256.Size

// sealDomain separates this encoding from every other use of the secret; the suffix changes with the encoding.
const sealDomain = "paratech.hub.v1.SealedCalls/1"

// Errors of the service (ARCH-18).
var (
	// ErrInvalidKey means a configured key breaks a rule (version 0, short secret, duplicate version).
	ErrInvalidKey = errors.New("invalid seal key")

	// ErrBadSeal means a seal does not match the calls and their binding, or names an unknown key version.
	ErrBadSeal = errors.New("seal does not match")
)

// Key is one seal secret with its version (1..255). The version is the first byte of every seal made with it.
type Key struct {
	Version byte
	Secret  []byte
}

// Binding is what a seal is bound to besides the calls: the caller, the thread, the profile and the catalog version.
type Binding struct {
	Owner          shared.APIKeyID
	ThreadID       string
	Profile        values.Profile
	CatalogVersion uint32
}

// Sealer makes and checks seals. It is immutable and safe for concurrent use.
type Sealer struct {
	current byte
	secrets map[byte][]byte
}

// New builds a sealer that signs with current and also accepts seals of the older keys (rotation). Every secret must
// have at least MinSecretBytes bytes and every version must be 1..255 and unique. The secrets are copied.
func New(current Key, older ...Key) (*Sealer, error) {
	s := &Sealer{current: current.Version, secrets: map[byte][]byte{}}
	for _, k := range append([]Key{current}, older...) {
		switch {
		case k.Version == 0:
			return nil, fmt.Errorf("%w: version must be 1..255", ErrInvalidKey)
		case len(k.Secret) < MinSecretBytes:
			return nil, fmt.Errorf("%w: secret of version %d is shorter than %d bytes", ErrInvalidKey, k.Version, MinSecretBytes)
		}
		if _, dup := s.secrets[k.Version]; dup {
			return nil, fmt.Errorf("%w: version %d is configured twice", ErrInvalidKey, k.Version)
		}
		s.secrets[k.Version] = append([]byte(nil), k.Secret...)
	}
	return s, nil
}

// Seal returns the seal of calls under b with the current key.
func (s *Sealer) Seal(b Binding, calls []values.ToolCall) []byte {
	return append([]byte{s.current}, mac(s.secrets[s.current], b, calls)...)
}

// Verify checks seal against calls under b in constant time; a seal of an older configured key is accepted. A wrong
// length, an unknown version or a mismatch is ErrBadSeal.
func (s *Sealer) Verify(b Binding, calls []values.ToolCall, seal []byte) error {
	if len(seal) != Size {
		return ErrBadSeal
	}
	secret, ok := s.secrets[seal[0]]
	if !ok {
		return ErrBadSeal
	}
	if !hmac.Equal(seal[1:], mac(secret, b, calls)) {
		return ErrBadSeal
	}
	return nil
}

// String prints the sealer without its secrets. Value receivers, so a dereferenced sealer is redacted too.
func (s Sealer) String() string {
	return fmt.Sprintf("seal.Sealer{Current:%d Keys:%d}", s.current, len(s.secrets))
}

// GoString is String for %#v.
func (s Sealer) GoString() string { return s.String() }

// mac is HMAC-SHA256 over the length-prefixed fields of the binding and the calls.
func mac(secret []byte, b Binding, calls []values.ToolCall) []byte {
	m := hmac.New(sha256.New, secret)
	write(m, sealDomain)
	write(m, string(b.Owner))
	write(m, b.ThreadID)
	write(m, string(b.Profile))
	write(m, strconv.FormatUint(uint64(b.CatalogVersion), 10))
	write(m, strconv.Itoa(len(calls)))
	for _, c := range calls {
		write(m, c.CallID())
		write(m, c.Tool())
		write(m, c.Arguments())
	}
	return m.Sum(nil)
}

func write(h hash.Hash, field string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(field)))
	h.Write(n[:])
	h.Write([]byte(field))
}
