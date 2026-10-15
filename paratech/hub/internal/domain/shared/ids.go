package shared

import "fmt"

// apiKeyIDLen is the length of an API key id: 8 random bytes in lowercase hex.
const apiKeyIDLen = 16

// APIKeyID is the public identifier of a per-tester Hub API key (PRD section 12). It identifies the authenticated
// caller across contexts: quota and idempotency keys are namespaced per API key (ADR-0005(d)). It is not a secret.
type APIKeyID string

// String returns the identifier as a plain string.
func (id APIKeyID) String() string { return string(id) }

// Validate reports whether the identifier has the issued shape: exactly 16 lowercase hex characters.
func (id APIKeyID) Validate() error {
	if len(id) != apiKeyIDLen || !isLowerHex(string(id)) {
		return fmt.Errorf("%w: API key id must be %d lowercase hex characters", ErrInvalidID, apiKeyIDLen)
	}
	return nil
}

// isLowerHex reports whether s is non-empty and consists only of 0-9 and a-f.
func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
