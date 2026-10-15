package values

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/shared"
)

func TestParseHubAPIKey(t *testing.T) {
	tests := []struct {
		name, raw, want string
		ok              bool
	}{
		{"plain", "la_test_0123456789abcdef", "la_test_0123456789abcdef", true},
		{"pasted with a line break", "  la_key-1\r\n", "la_key-1", true},
		{"every printable ascii", "!~#$%&'()*+,-./09:;<=>?@AZ[\\]^_`az{|}", "!~#$%&'()*+,-./09:;<=>?@AZ[\\]^_`az{|}", true},
		{"at the limit", strings.Repeat("k", MaxHubAPIKeyLen), strings.Repeat("k", MaxHubAPIKeyLen), true},
		{"empty", "", "", false},
		{"blank", " \t\r\n", "", false},
		{"over the limit", strings.Repeat("k", MaxHubAPIKeyLen+1), "", false},
		{"inner space", "abc def", "", false},
		{"header injection", "abc\r\nX-Evil: 1", "", false},
		{"control character", "abc\x00", "", false},
		{"delete", "abc\x7f", "", false},
		{"non-ascii", "ключ", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHubAPIKey(tt.raw)
			if tt.ok != (err == nil) || got != tt.want {
				t.Fatalf("ParseHubAPIKey = %q, %v; want %q ok %v", got, err, tt.want, tt.ok)
			}
			if err != nil {
				if !errors.Is(err, shared.ErrInvalidSecret) {
					t.Fatalf("error %v does not wrap shared.ErrInvalidSecret", err)
				}
				if tt.raw != "" && strings.Contains(err.Error(), tt.raw) {
					t.Fatalf("error %q quotes the value", err)
				}
			}
			if tt.ok && !ValidHubAPIKey(got) {
				t.Fatal("a parsed key is not valid")
			}
		})
	}
	if ValidHubAPIKey(" la_key") {
		t.Fatal("ValidHubAPIKey trims: a stored key must be sendable as it is")
	}
}
