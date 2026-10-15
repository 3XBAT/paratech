package shared

import (
	"errors"
	"strings"
	"testing"
)

func TestAPIKeyIDValidate(t *testing.T) {
	if err := APIKeyID("0123456789abcdef").Validate(); err != nil {
		t.Fatalf("valid id: %v", err)
	}
	for _, bad := range []string{"", "0123456789abcde", "0123456789abcdef0", "0123456789ABCDEF", "0123456789abcdeg", strings.Repeat(" ", 16)} {
		if err := APIKeyID(bad).Validate(); !errors.Is(err, ErrInvalidID) {
			t.Errorf("Validate(%q) = %v, want ErrInvalidID", bad, err)
		}
	}
}

func TestIsLowerHex(t *testing.T) {
	for s, want := range map[string]bool{"": false, "0af9": true, "0AF9": false, "x": false, "a-b": false} {
		if got := IsLowerHex(s); got != want {
			t.Errorf("IsLowerHex(%q) = %v, want %v", s, got, want)
		}
	}
}
