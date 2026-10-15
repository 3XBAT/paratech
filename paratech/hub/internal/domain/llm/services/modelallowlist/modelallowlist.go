// Package modelallowlist is the server-side model allowlist (PRD section 13.2): a client names a model only by an
// alias, and only the aliases configured on the Hub resolve to a provider model id and an output cap.
package modelallowlist

import (
	"errors"
	"fmt"
)

// Errors of the allowlist (ARCH-18).
var (
	// ErrModelNotAllowed means the alias is not in the allowlist (also when it is malformed).
	ErrModelNotAllowed = errors.New("model is not allowed")

	// ErrInvalidEntry means a configured entry breaks a rule, or the list is empty.
	ErrInvalidEntry = errors.New("invalid model allowlist entry")

	// ErrInvalidMaxOutput means a requested output cap is negative.
	ErrInvalidMaxOutput = errors.New("invalid max output tokens")
)

// Entry rules: the alias format is ADR-0005(b); MaxOutputLimit is far above any current model (128K output tokens)
// and keeps a reservation based on the cap small.
const (
	maxAliasLen    = 64
	maxModelLen    = 128
	MaxOutputLimit = 1 << 20
)

// Entry is one configured model.
type Entry struct {
	// Alias is the name a client sends: 1-64 characters of [a-z0-9_-], starting with a letter or digit.
	Alias string
	// Model is the provider model id: 1-128 characters of [A-Za-z0-9._-].
	Model string
	// MaxOutput is the output cap of the alias: 1..MaxOutputLimit tokens.
	MaxOutput int64
}

// List is a validated allowlist. It is immutable and safe for concurrent use.
type List struct {
	entries map[string]Entry
}

// New validates entries: every entry follows the rules of Entry, aliases are unique and the list is not empty.
func New(entries []Entry) (*List, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: the allowlist is empty", ErrInvalidEntry)
	}
	m := make(map[string]Entry, len(entries))
	for i, e := range entries {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		if _, dup := m[e.Alias]; dup {
			return nil, fmt.Errorf("entry %d: %w: duplicate alias %q", i+1, ErrInvalidEntry, e.Alias)
		}
		m[e.Alias] = e
	}
	return &List{entries: m}, nil
}

// Resolve returns the provider model id of alias and the effective output cap: requestedMaxOutput, or the alias cap
// when it is 0 or larger (clamped silently, ADR-0005(b)).
func (l *List) Resolve(alias string, requestedMaxOutput int64) (model string, maxOutput int64, err error) {
	if requestedMaxOutput < 0 {
		return "", 0, ErrInvalidMaxOutput
	}
	e, ok := l.entries[alias]
	if !ok {
		// The alias is client input: it is not quoted in the error.
		return "", 0, ErrModelNotAllowed
	}
	if requestedMaxOutput == 0 || requestedMaxOutput > e.MaxOutput {
		return e.Model, e.MaxOutput, nil
	}
	return e.Model, requestedMaxOutput, nil
}

func (e Entry) validate() error {
	if !validAlias(e.Alias) {
		return fmt.Errorf("%w: alias must be 1-%d characters of a-z, 0-9, '_' and '-' starting with a letter or digit",
			ErrInvalidEntry, maxAliasLen)
	}
	if !validModel(e.Model) {
		return fmt.Errorf("%w: model of alias %q must be 1-%d characters of A-Z, a-z, 0-9, '.', '_' and '-'",
			ErrInvalidEntry, e.Alias, maxModelLen)
	}
	if e.MaxOutput <= 0 || e.MaxOutput > MaxOutputLimit {
		return fmt.Errorf("%w: max output of alias %q must be within 1..%d", ErrInvalidEntry, e.Alias, MaxOutputLimit)
	}
	return nil
}

func validAlias(s string) bool {
	if len(s) == 0 || len(s) > maxAliasLen || !isLowerAlnum(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isLowerAlnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func validModel(s string) bool {
	if len(s) == 0 || len(s) > maxModelLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isLowerAlnum(c) && (c < 'A' || c > 'Z') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isLowerAlnum(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') }
