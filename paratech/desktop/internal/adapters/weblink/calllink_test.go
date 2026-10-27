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
		// Refused by ParseCallLink already: %US is not a hex escape. The env check is proven in TestCheckCallLinkEnv.
		{"non-hex environment variable", "https://example.com/%USERPROFILE%"},
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

func useEnviron(t *testing.T, env ...string) {
	t.Helper()
	prev := environ
	environ = func() []string { return env }
	t.Cleanup(func() { environ = prev })
}

// Fake environment for the checks: names only, values obviously fake.
var fakeEnv = []string{
	"=C:=C:\\fake", "USERNAME=fake-user", "Db_Password=fake", "DEADBEEF2=fake", "PROGRAMFILES(X86)=C:\\fake",
	"DB-TOKEN=fake", "AB.KEY=fake", "AD$SECRET=fake",
}

func TestCheckCallLinkEnv(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		refuse bool
	}{
		{"hex-prefixed set variable", "https://example.zoom.us/j/1?x=%DB_PASSWORD%20", true},
		{"set variable, other case", "https://example.zoom.us/j/1?x=%db_password%20", true},
		{"second opener names the variable", "https://example.zoom.us/j/1?x=%20%DB_PASSWORD%20", true},
		{"set variable with a hyphen", "https://example.zoom.us/j/1?x=%DB-TOKEN%20", true},
		{"set variable with a dot", "https://example.zoom.us/j/1?x=%AB.KEY%20", true},
		{"set variable with a dollar", "https://example.zoom.us/j/1?x=%AD$SECRET%20", true},
		{"set variable with a hyphen, other case", "https://example.zoom.us/j/1?x=%db-token%20", true},
		{"hex-prefixed unset name with punctuation", "https://example.com/a?x=%DB-OTHER%20", false},
		{"Teams quotes", teamsCallLink, false},
		{"colon escapes", "https://example.com/a%3ab%3ac", false},
		{"hex-prefixed unset variable", "https://example.com/a?x=%DEADBEEF%20", false},
		{"zoom", zoomCallLink, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useEnviron(t, fakeEnv...)
			mustCallLink(t, tt.in) // a valid link, so a refusal comes from the env check
			got, err := checkCallLink(tt.in)
			if tt.refuse && !errors.Is(err, ErrRefused) {
				t.Fatalf("checkCallLink(%q) = %q, %v; want ErrRefused", tt.in, got, err)
			}
			if !tt.refuse && (err != nil || got != tt.in) {
				t.Fatalf("checkCallLink(%q) = %q, %v; want it unchanged", tt.in, got, err)
			}
		})
	}
}

// The helper alone, on link-like strings ParseCallLink would refuse anyway.
func TestHasEnvReference(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"https://example.com/%USERNAME%", true},
		{"https://example.com/%username%x", true},
		{"https://example.com/%ProgramFiles(x86)%", true},
		{"https://example.com/%a%USERNAME%", true},
		{"https://example.com/%DB-TOKEN%", true},
		{"https://example.com/%ab.key%", true},
		{"https://example.com/%AD$SECRET%", true},
		{"https://example.com/%C:%", false},
		{"https://example.com/%%", false},
		{"https://example.com/%FOO%", false},
		// No longer gated by a character set: false only because no key is "USER NAME".
		{"https://example.com/%USER NAME%", false},
		{"https://example.com/%DB-OTHER%", false},
		{"https://example.com/%USERNAME", false},
		{"https://example.com/%22Tid%22", false},
	}
	for _, tt := range tests {
		if got := hasEnvReference(tt.in, fakeEnv); got != tt.want {
			t.Errorf("hasEnvReference(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestEnvKeys(t *testing.T) {
	got := envKeys([]string{"=C:=C:\\fake", "A=1", "B==x", "NOVALUE", "", "=", "C="})
	want := []string{"=C:", "A", "B", "NOVALUE", "=", "C"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("envKeys = %q, want %q", got, want)
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
