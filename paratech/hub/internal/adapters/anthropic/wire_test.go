package anthropic

import (
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	tests := map[string]time.Duration{
		"7": 7 * time.Second, " 12 ": 12 * time.Second, "600": 10 * time.Minute, "100000": 10 * time.Minute,
		"9223372036854775807": 10 * time.Minute, "99999999999999999999": 0,
		"": 0, "0": 0, "-1": 0, "1.5": 0, "soon": 0, "Wed, 21 Oct 2026 07:28:00 GMT": 0,
	}
	for in, want := range tests {
		if got := retryAfter(in); got != want {
			t.Errorf("retryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}
