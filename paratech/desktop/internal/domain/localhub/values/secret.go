package values

import "fmt"

// SecretName names a secret slot of the local Hub in the secret store (ADR-0036 section 3). The domain knows only
// names; the values are written by the owner and read only by the adapters that start the Hub.
type SecretName string

// Secret slots: a closed list.
const (
	SecretProviderKey SecretName = "provider-key" // HUB_ANTHROPIC_API_KEY
	SecretDatabaseURL SecretName = "database-url" // HUB_DATABASE_URL, holds the database password
	SecretSealKeys    SecretName = "seal-keys"    // HUB_AGENT_SEAL_KEYS, generated, never typed in
)

// ParseTypedSecret accepts the names of the slots the owner types in: provider-key and database-url. Any other name,
// seal-keys included, returns ErrUnknownSecret. The rejected name is not quoted: a value sent in the wrong field must
// not reach a log.
func ParseTypedSecret(name string) (SecretName, error) {
	switch n := SecretName(name); n {
	case SecretProviderKey, SecretDatabaseURL:
		return n, nil
	}
	return "", fmt.Errorf("%w: want %s or %s", ErrUnknownSecret, SecretProviderKey, SecretDatabaseURL)
}

// String returns the slot name.
func (n SecretName) String() string { return string(n) }
