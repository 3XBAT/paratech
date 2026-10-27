package values

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCallLinkAccepts(t *testing.T) {
	tests := []struct {
		name, in, want, host string
	}{
		{"zoom with password", "https://us02web.zoom.us/j/81234567890?pwd=AbCdEfGh123", "https://us02web.zoom.us/j/81234567890?pwd=AbCdEfGh123", "us02web.zoom.us"},
		{"teams with escapes", "https://teams.microsoft.com/l/meetup-join/19%3ameeting_ZmFrZQ%40thread.v2/0?context=%7b%22Tid%22%3a%22x%22%7d",
			"https://teams.microsoft.com/l/meetup-join/19%3ameeting_ZmFrZQ%40thread.v2/0?context=%7b%22Tid%22%3a%22x%22%7d", "teams.microsoft.com"},
		{"meet", "https://meet.google.com/abc-defg-hij", "https://meet.google.com/abc-defg-hij", "meet.google.com"},
		{"telemost", "https://telemost.yandex.ru/j/12345678901234", "https://telemost.yandex.ru/j/12345678901234", "telemost.yandex.ru"},
		{"http", "http://bbb.example.edu/b/abc-123", "http://bbb.example.edu/b/abc-123", "bbb.example.edu"},
		{"no path", "https://meet.jit.si", "https://meet.jit.si", "meet.jit.si"},
		{"query without path", "https://example.com?room=1", "https://example.com?room=1", "example.com"},
		{"trimmed", " \t\r\nhttps://meet.google.com/abc-defg-hij\r\n ", "https://meet.google.com/abc-defg-hij", "meet.google.com"},
		// Scheme and host are case-insensitive: canonical form lowercases them, the path keeps its case.
		{"uppercase scheme and host", "HTTPS://Meet.Google.COM/ABC-defg", "https://meet.google.com/ABC-defg", "meet.google.com"},
		{"sub-delims and at in path", "https://example.com/a;b=c/d@e!$&()*+,:~_.-?x=/?", "https://example.com/a;b=c/d@e!$&()*+,:~_.-?x=/?", "example.com"},
		{"digits in labels", "https://a1.b2c.example9.com/x", "https://a1.b2c.example9.com/x", "a1.b2c.example9.com"},
		{"exactly the limit", "https://example.com/" + strings.Repeat("a", MaxCallLinkBytes-len("https://example.com/")),
			"https://example.com/" + strings.Repeat("a", MaxCallLinkBytes-len("https://example.com/")), "example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCallLink(tc.in)
			if err != nil {
				t.Fatalf("ParseCallLink: %v", err)
			}
			if got.String() != tc.want || got.Host() != tc.host || got.IsZero() {
				t.Fatalf("got %q host %q, want %q host %q", got.String(), got.Host(), tc.want, tc.host)
			}
			// The canonical form parses to itself.
			again, err := ParseCallLink(got.String())
			if err != nil || again != got {
				t.Fatalf("canonical form does not parse to itself: %v, %v", again, err)
			}
		})
	}
}

func TestParseCallLinkRefuses(t *testing.T) {
	tests := []struct {
		name, in, rule string
	}{
		{"empty", "", "empty"},
		{"blank", " \t ", "empty"},
		{"javascript", "javascript:alert(1)", "no http or https scheme"},
		{"javascript with slashes", "javascript://example.com/%0aalert(1)", "scheme is not http or https"},
		{"file", "file:///C:/Windows/system32/calc.exe", "scheme is not http or https"},
		{"data", "data:text/html;base64,PHNjcmlwdD4=", "no http or https scheme"},
		{"ftp", "ftp://example.com/a", "scheme is not http or https"},
		{"no scheme", "meet.google.com/abc-defg-hij", "no http or https scheme"},
		{"user info", "https://user@zoom.us/j/1", "user info not allowed"},
		{"user info hiding the host", "https://zoom.us@evil.example/j/1", "user info not allowed"},
		{"user and password", "https://user:pass@zoom.us/j/1", "user info not allowed"},
		{"port", "https://zoom.us:8443/j/1", "port not allowed"},
		{"empty port", "https://zoom.us:/j/1", "port not allowed"},
		{"fragment", "https://meet.google.com/abc#x", "fragment not allowed"},
		{"space", "https://meet.google.com/abc def", "not printable ASCII without spaces"},
		{"double quote", "https://meet.google.com/abc\"def", "forbidden character"},
		{"single quote", "https://meet.google.com/abc'def", "forbidden character"},
		{"backtick", "https://meet.google.com/abc`def", "forbidden character"},
		{"angle bracket", "https://meet.google.com/<script>", "forbidden character"},
		{"backslash", "https://evil.example\\@zoom.us/", "forbidden character"},
		{"caret", "https://meet.google.com/a^b", "forbidden character"},
		{"brace", "https://meet.google.com/a{b}", "forbidden character"},
		{"pipe", "https://meet.google.com/a|b", "forbidden character"},
		{"IDN unicode", "https://зум.рф/j/1", "not printable ASCII without spaces"},
		{"IDN punycode", "https://xn--h1alfs.xn--p1ai/j/1", "international host not allowed"},
		{"IPv4", "https://192.168.0.1/j/1", "IP address host not allowed"},
		{"short IPv4", "https://127.1/", "IP address host not allowed"},
		{"hex IPv4", "https://0x7f.0.0.0x1/", "IP address host not allowed"},
		{"numeric last label", "https://example.123/", "IP address host not allowed"},
		{"IPv6", "https://[::1]/j/1", "IP address host not allowed"},
		{"no dot", "https://localhost/j/1", "host has no dot"},
		{"empty label", "https://meet..google.com/", "host has an empty label"},
		{"trailing dot", "https://meet.google.com./", "host has an empty label"},
		{"leading dot", "https://.google.com/", "host has an empty label"},
		{"hyphen label", "https://-meet.google.com/", "host label starts or ends with a hyphen"},
		{"underscore host", "https://meet_x.google.com/", "host has a character outside a-z 0-9 . -"},
		{"percent in host", "https://meet%2egoogle.com/", "host has a character outside a-z 0-9 . -"},
		{"no host", "https:///j/1", "no host"},
		{"host too long", "https://" + strings.Repeat("a.", 127) + "com/", "host too long"},
		{"label too long", "https://" + strings.Repeat("a", 64) + ".com/", "host label too long"},
		{"bad percent", "https://example.com/a%zz", "bad percent escape"},
		{"cut percent", "https://example.com/a%4", "bad percent escape"},
		{"percent at end", "https://example.com/a%", "bad percent escape"},
		{"square bracket in path", "https://example.com/a[0]", "character not allowed in path or query"},
		{"too long", "https://example.com/" + strings.Repeat("a", MaxCallLinkBytes), "longer than 2048 bytes"},
		{"newline inside", "https://example.com/a\nb", "not printable ASCII without spaces"},
		{"tab inside", "https://example.com/a\tb", "not printable ASCII without spaces"},
		{"NUL", "https://example.com/a\x00", "not printable ASCII without spaces"},
		{"DEL", "https://example.com/a\x7f", "not printable ASCII without spaces"},
		{"bidi override", "https://example.com/\u202egnp.exe", "not printable ASCII without spaces"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCallLink(tc.in)
			if !errors.Is(err, ErrInvalidCallLink) {
				t.Fatalf("ParseCallLink = %q, %v; want ErrInvalidCallLink", got.String(), err)
			}
			if !got.IsZero() {
				t.Errorf("refused link is not zero: %q", got.String())
			}
			if want := ErrInvalidCallLink.Error() + ": " + tc.rule; err.Error() != want {
				t.Errorf("error %q, want %q", err, want)
			}
		})
	}
}

// The error names the rule only: untrusted text never reaches a log through it.
func TestParseCallLinkErrorDoesNotQuote(t *testing.T) {
	const secret = "pwd=TopSecret42"
	for _, in := range []string{
		"https://user@zoom.us/j/1?" + secret,
		"https://zoom.us:1/j/1?" + secret,
		"https://zoom.us/j/1?" + secret + "#frag",
		"https://zoom.us/j/1?" + secret + "%zz",
	} {
		_, err := ParseCallLink(in)
		if err == nil || strings.Contains(err.Error(), "TopSecret") || strings.Contains(err.Error(), "zoom") {
			t.Errorf("error %v quotes the text", err)
		}
	}
}

func TestCallLinkZeroValue(t *testing.T) {
	var l CallLink
	if !l.IsZero() || l.String() != "" || l.Host() != "" {
		t.Fatalf("zero CallLink = %q %q", l.String(), l.Host())
	}
}
