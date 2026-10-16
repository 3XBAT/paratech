package values

import (
	"fmt"
	"strings"
)

// Length bounds of the model labels.
const (
	MaxModelNameLen    = 64
	MaxModelVersionLen = 32
	MaxCatalogIDLen    = 64
	MaxQuantizationLen = 16
	MaxCapabilityLen   = 32
	MaxCapabilities    = 16
)

// MaxModelSizeBytes bounds the size of one model file; it is far above any whisper.cpp model.
const MaxModelSizeBytes int64 = 64 << 30

// sha256HexLen is the length of a SHA-256 digest in hex.
const sha256HexLen = 64

// ModelName is the name of a speech-to-text model, for example "base" or "large-v3-turbo" (PRD section 8).
type ModelName string

// Validate reports whether the name is a label of ASCII letters, digits, '.', '_' and '-' that starts and ends with a
// letter or digit.
func (n ModelName) Validate() error {
	return validateLabel(ErrInvalidModelName, string(n), MaxModelNameLen)
}

// String returns the name.
func (n ModelName) String() string { return string(n) }

// ModelVersion is the model's own version, for example "1" or "2024-03"; not the aggregate version (PRD section 8).
type ModelVersion string

// Validate applies the rules of ModelName.Validate.
func (v ModelVersion) Validate() error {
	return validateLabel(ErrInvalidModelVersion, string(v), MaxModelVersionLen)
}

// String returns the version.
func (v ModelVersion) String() string { return string(v) }

// CatalogID identifies an entry of the catalog manifest (PRD section 8.1). Empty means "not from the catalog".
type CatalogID string

// Validate applies the rules of ModelName.Validate.
func (id CatalogID) Validate() error {
	return validateLabel(ErrInvalidCatalogID, string(id), MaxCatalogIDLen)
}

// String returns the id.
func (id CatalogID) String() string { return string(id) }

// Quantization labels the weight format, for example "q5_1" or "f16" (PRD section 8.1). Empty means unknown.
type Quantization string

// Validate reports whether the label is empty or lowercase ASCII letters, digits and '_'.
func (q Quantization) Validate() error {
	if q == "" {
		return nil
	}
	if len(q) > MaxQuantizationLen {
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidQuantization, MaxQuantizationLen)
	}
	for _, r := range string(q) {
		if !isLowerAlnum(r) && r != '_' {
			return fmt.Errorf("%w: %q", ErrInvalidQuantization, string(q))
		}
	}
	return nil
}

// String returns the label.
func (q Quantization) String() string { return string(q) }

// Capability labels what a model can do, for example "multilingual" or a language code (PRD section 8).
type Capability string

// Capabilities that describe the languages of a whisper.cpp model.
const (
	CapabilityMultilingual Capability = "multilingual"
	CapabilityEnglish      Capability = "en" // a language code, as for any language a model is limited to
)

// Validate reports whether the label is lowercase ASCII letters, digits, '_' and '-', starting with a letter or digit.
func (c Capability) Validate() error {
	if c == "" || len(c) > MaxCapabilityLen {
		return fmt.Errorf("%w: empty or longer than %d bytes", ErrInvalidCapability, MaxCapabilityLen)
	}
	for i, r := range string(c) {
		if isLowerAlnum(r) || (i > 0 && (r == '_' || r == '-')) {
			continue
		}
		return fmt.Errorf("%w: %q", ErrInvalidCapability, string(c))
	}
	return nil
}

// ValidateCapabilities checks every label, the count and that no label repeats.
func ValidateCapabilities(caps []Capability) error {
	if len(caps) > MaxCapabilities {
		return fmt.Errorf("%w: more than %d capabilities", ErrInvalidCapability, MaxCapabilities)
	}
	seen := make(map[Capability]bool, len(caps))
	for _, c := range caps {
		if err := c.Validate(); err != nil {
			return err
		}
		if seen[c] {
			return fmt.Errorf("%w: %q repeats", ErrInvalidCapability, string(c))
		}
		seen[c] = true
	}
	return nil
}

// SHA256 is a SHA-256 digest in lowercase hex, the integrity check of a model file (PRD section 8.1, section 14 case 13).
type SHA256 string

// ParseSHA256 accepts 64 hex digits in any letter case and returns the canonical lowercase form.
func ParseSHA256(s string) (SHA256, error) {
	sum := SHA256(strings.ToLower(s))
	if err := sum.Validate(); err != nil {
		return "", err
	}
	return sum, nil
}

// Validate reports whether the digest is exactly 64 lowercase hex digits.
func (s SHA256) Validate() error {
	if len(s) != sha256HexLen {
		return fmt.Errorf("%w: want %d hex digits, got %d bytes", ErrInvalidSHA256, sha256HexLen, len(s))
	}
	for _, r := range string(s) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("%w: not lowercase hex", ErrInvalidSHA256)
		}
	}
	return nil
}

// String returns the hex digest.
func (s SHA256) String() string { return string(s) }

// ValidateSize reports whether size is a plausible model file size.
func ValidateSize(size int64) error {
	if size <= 0 || size > MaxModelSizeBytes {
		return fmt.Errorf("%w: %d bytes", ErrInvalidSize, size)
	}
	return nil
}

// validateLabel is the shared rule of names, versions and catalog ids.
func validateLabel(kind error, s string, maxLen int) error {
	if s == "" || len(s) > maxLen {
		return fmt.Errorf("%w: empty or longer than %d bytes", kind, maxLen)
	}
	for i, r := range s {
		if isAlnum(r) {
			continue
		}
		edge := i == 0 || i == len(s)-1
		if !edge && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return fmt.Errorf("%w: %q", kind, s)
	}
	return nil
}

func isAlnum(r rune) bool { return isLowerAlnum(r) || (r >= 'A' && r <= 'Z') }

func isLowerAlnum(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') }
