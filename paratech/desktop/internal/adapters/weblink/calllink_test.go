package weblink

import (
	"context"
	"errors"
	"strings"
	"testing"

	schedvalues "paratech/desktop/internal/domain/schedule/values"
)

// Fake but realistic call links: the shapes Zoom and Teams invitations have.
const (
	zoomCallLink  = "https://example.zoom.us/j/123456789?pwd=abc123"
	teamsCallLink = "https://teams.microsoft.com/l/meetup-join/19%3ameeting_ZmFrZQ%40thread.v2/0" +
		"?context=%7b%22Tid%22%3a%22fake-tenant%22%7d"
)

func TestCheckCallLinkAllows(t *testing.T) {
	for _, s := range []string{
		zoomCallLink,
		teamsCallLink,
		"http://bbb.example.edu/b/room-42",
		"https://meet.example.com",
		"https://meet.example.com/abc-defg-hij?authuser=0&hs=1",
		"https://example.com/" + strings.Repeat("a", schedvalues.MaxCallLinkBytes-len("https://example.com/")),
	} {
		got, err := checkCallLink(s)
		if err != nil || got != s {
			t.Errorf("checkCallLink(%q) = %q, %v; want it unchanged", s, got, err)
		}
	}
}

func TestCheckCallLinkRefuses(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"CR LF injection", "https://example.com/a\r\nURL=https://evil.example.com/"},
		{"LF", "https://example.com/a\nb"},
		{"CR", "https://example.com/a\rb"},
		{"NUL", "https://example.com/a\x00"},
		{"tab", "https://example.com/a\tb"},
		{"double quote", `https://example.com/a"b`},
		{"single quote", "https://example.com/a'b"},
		{"space inside", "https://example.com/a b"},
		{"leading space", " https://example.com/a"},
		{"trailing space", "https://example.com/a "},
		{"non-ASCII", "https://example.com/aé"},
		{"ftp", "ftp://example.com/a"},
		{"file", "file:///C:/Windows/System32/calc.exe"},
		{"javascript", "javascript:alert(1)"},
		{"no scheme", "example.com/a"},
		{"mailto", "mailto:someone@example.com"},
		{"port", "https://example.com:443/a"},
		{"user info", "https://user@example.com/a"},
		{"user and password", "https://user:pw@example.com/a"},
		{"bare percent", "https://example.com/a%"},
		{"short escape", "https://example.com/a%2"},
		{"non-hex escape", "https://example.com/a%zz"},
		{"environment variable", "https://example.com/%USERPROFILE%"},
		{"fragment", "https://example.com/a#b"},
		{"uppercase scheme", "HTTPS://example.com/a"},
		{"uppercase host", "https://Example.com/a"},
		{"IPv6 host", "https://[::1]/a"},
		{"IPv4 host", "https://127.0.0.1/a"},
		{"over length", "https://example.com/" + strings.Repeat("a", schedvalues.MaxCallLinkBytes)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := checkCallLink(tt.in); !errors.Is(err, ErrRefused) {
				t.Fatalf("checkCallLink(%q) = %q, %v; want ErrRefused", tt.in, got, err)
			}
		})
	}
}

// A refused call link never reaches the launcher, on every system: the zero value is no link.
func TestOpenCallLinkRefusesTheZeroValue(t *testing.T) {
	if err := New().OpenCallLink(context.Background(), schedvalues.CallLink{}); !errors.Is(err, ErrRefused) {
		t.Fatalf("OpenCallLink(zero) = %v, want ErrRefused", err)
	}
}

func mustCallLink(t *testing.T, s string) schedvalues.CallLink {
	t.Helper()
	l, err := schedvalues.ParseCallLink(s)
	if err != nil {
		t.Fatalf("ParseCallLink(%q): %v", s, err)
	}
	return l
}
