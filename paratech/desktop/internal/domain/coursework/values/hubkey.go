package values

import (
	"fmt"
	"strings"

	"paratech/desktop/internal/domain/shared"
)

// MaxHubAPIKeyLen is the longest Hub API key: the blob limit of the credential store (wincred: 5*512 bytes).
const MaxHubAPIKeyLen = 5 * 512

// ValidHubAPIKey reports whether key can be sent as "Authorization: Bearer <key>" (PRD section 12): 1 to
// MaxHubAPIKeyLen printable ASCII characters without spaces, so the header cannot be split or extended.
func ValidHubAPIKey(key string) bool {
	if key == "" || len(key) > MaxHubAPIKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// ParseHubAPIKey is the key as the user entered it: surrounding blanks and line breaks of a paste are dropped (a key
// has none), the rest must be ValidHubAPIKey. The error wraps shared.ErrInvalidSecret and never quotes the value.
func ParseHubAPIKey(raw string) (string, error) {
	key := strings.Trim(raw, " \t\r\n")
	if !ValidHubAPIKey(key) {
		return "", fmt.Errorf("%w: hub api key must be 1-%d printable ASCII characters without spaces",
			shared.ErrInvalidSecret, MaxHubAPIKeyLen)
	}
	return key, nil
}
