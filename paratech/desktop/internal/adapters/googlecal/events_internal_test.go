package googlecal

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSummary(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "Алгебра", "Алгебра"},
		{"trimmed and collapsed", "  Алгебра \t  7 ", "Алгебра 7"},
		{"line breaks", "a\r\nb\nc", "a b c"},
		{"line and paragraph separators", "a\u2028b\u2029c", "a b c"},
		{"C1 control", "a\u0085b\u009bc", "a b c"},
		{"bidi override", "abc\u202Eexe.txt", "abcexe.txt"},
		{"bidi marks and isolates", "\u200Ea\u200F\u061Cb\u2066c\u2067d\u2068e\u2069\u202A\u202B\u202C\u202D", "abcde"},
		{"invalid utf-8", "a\xffb", "ab"},
		{"only controls", "\u202E\u2028\n\t", "fallback"},
		{"empty", "", "fallback"},
		{"long", strings.Repeat("я", maxSummaryRunes+50), strings.Repeat("я", maxSummaryRunes)},
		{"cut before a space", strings.Repeat("я", maxSummaryRunes-1) + " b", strings.Repeat("я", maxSummaryRunes-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summary(tt.in, "fallback")
			if got != tt.want {
				t.Fatalf("summary(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) || utf8.RuneCountInString(got) > maxSummaryRunes {
				t.Fatalf("summary %q is invalid or too long", got)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"7", 7 * time.Second},
		{" 30 ", 30 * time.Second},
		{"0", 0},
		{"-5", 0},
		{"999999999999", maxRetryAfter},
		{"soon", 0},
		{now.Add(2 * time.Minute).Format(http.TimeFormat), 2 * time.Minute},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{now.Add(48 * time.Hour).Format(http.TimeFormat), maxRetryAfter},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestStatusError(t *testing.T) {
	tests := []struct {
		status  int
		reasons []string
		reason  error
	}{
		{429, nil, ErrRateLimited},
		{403, []string{"quotaExceeded"}, ErrRateLimited},
		{403, []string{"dailyLimitExceeded"}, ErrRateLimited},
		{403, []string{"insufficientPermissions"}, ErrForbidden},
		{404, nil, ErrNotFound},
		{410, nil, ErrNotFound},
		{409, nil, ErrConflict},
		{412, nil, ErrConflict},
		{503, nil, ErrUnavailable},
		{422, nil, ErrRejected},
		{302, nil, ErrBadResponse},
	}
	for _, tt := range tests {
		e := statusError("op", tt.status, tt.reasons, time.Second)
		if e.reason != tt.reason || e.status != tt.status {
			t.Errorf("statusError(%d, %v) = %v, want %v", tt.status, tt.reasons, e, tt.reason)
		}
	}
}
