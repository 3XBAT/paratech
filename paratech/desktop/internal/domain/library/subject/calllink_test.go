package subject

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A fake call link: a reserved domain, a made-up meeting and password.
const (
	callLink     = "https://meet.example.org/j/1234567890?pwd=FakePwd42"
	callLinkHost = "meet.example.org"
	callLinkPwd  = "FakePwd42"
)

func TestParseCallLinkAccepts(t *testing.T) {
	tests := []struct {
		in, want, host string
	}{
		{callLink, callLink, callLinkHost},
		{"  HTTPS://Meet.Example.ORG/j/1?pwd=AbC  ", "https://meet.example.org/j/1?pwd=AbC", "meet.example.org"},
		{"http://bbb.university.example.edu/b/abc-def", "http://bbb.university.example.edu/b/abc-def", "bbb.university.example.edu"},
		{"https://teams.example.com/l/meetup-join/19%3ameeting_x%40thread.v2/0?context=%7b%22Tid%22%7d",
			"https://teams.example.com/l/meetup-join/19%3ameeting_x%40thread.v2/0?context=%7b%22Tid%22%7d", "teams.example.com"},
		{"https://a.example.org", "https://a.example.org", "a.example.org"},
		{"https://a.example.org?x=1", "https://a.example.org?x=1", "a.example.org"},
		{"https://a-b.example.org/!$&()*+,;=:@/~._-", "https://a-b.example.org/!$&()*+,;=:@/~._-", "a-b.example.org"},
		{"https://" + strings.Repeat("a", 63) + ".example.org/", "https://" + strings.Repeat("a", 63) + ".example.org/", strings.Repeat("a", 63) + ".example.org"},
	}
	for _, tt := range tests {
		got, err := ParseCallLink(tt.in)
		if err != nil || got.String() != tt.want || got.Host() != tt.host || got.IsZero() {
			t.Errorf("ParseCallLink(%q) = %q %q, %v", tt.in, got.String(), got.Host(), err)
		}
	}
	long := "https://a.example.org/" + strings.Repeat("x", MaxCallLinkBytes-len("https://a.example.org/"))
	if _, err := ParseCallLink(long); err != nil {
		t.Fatalf("link of %d bytes: %v", len(long), err)
	}
}

func TestParseCallLinkRejects(t *testing.T) {
	tests := []struct{ label, in string }{
		{"empty", "   "},
		{"too long", "https://a.example.org/" + strings.Repeat("x", MaxCallLinkBytes)},
		{"space inside", "https://a.example.org/a b"},
		{"newline", "https://a.example.org/a\nb"},
		{"non ascii", "https://a.example.org/путь"},
		{"quote", `https://a.example.org/"x"`},
		{"apostrophe", "https://a.example.org/'x'"},
		{"angle", "https://a.example.org/<script>"},
		{"backslash", `https://a.example.org\x`},
		{"caret", "https://a.example.org/^"},
		{"brace", "https://a.example.org/{x}"},
		{"pipe", "https://a.example.org/|"},
		{"backtick", "https://a.example.org/`x`"},
		{"fragment", "https://a.example.org/x#y"},
		{"no scheme", "meet.example.org/j/1"},
		{"javascript", "javascript://a.example.org/%0aalert(1)"},
		{"file", "file://a.example.org/c$/x"},
		{"ftp", "ftp://a.example.org/x"},
		{"no host", "https:///x"},
		{"user info", "https://user:" + callLinkPwd + "@a.example.org/"},
		{"port", "https://a.example.org:8443/"},
		{"ipv6", "https://[::1]/"},
		{"ipv4", "https://127.0.0.1/"},
		{"hex ipv4", "https://a.0x7f/"},
		{"no dot", "https://localhost/"},
		{"empty label", "https://a..example.org/"},
		{"trailing dot", "https://a.example.org./"},
		{"hyphen label", "https://-a.example.org/"},
		{"idn", "https://xn--e1afmkfd.example.org/"},
		{"underscore host", "https://a_b.example.org/"},
		{"percent host", "https://a%2eexample.org/"},
		{"long label", "https://" + strings.Repeat("a", 64) + ".example.org/"},
		{"bad escape", "https://a.example.org/%zz"},
		{"cut escape", "https://a.example.org/%4"},
		{"bracket in path", "https://a.example.org/[x]"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, err := ParseCallLink(tt.in)
			if !errors.Is(err, ErrInvalidCallLink) || !got.IsZero() {
				t.Fatalf("ParseCallLink = %q, %v", got.String(), err)
			}
			// The untrusted text never gets into an error.
			for _, part := range []string{"example", callLinkPwd, "localhost", "script"} {
				if strings.Contains(err.Error(), part) {
					t.Fatalf("error %q holds the typed text", err)
				}
			}
		})
	}
}

func TestSetAndClearCallLink(t *testing.T) {
	s := newSubject(t)
	if !s.CallLink().IsZero() {
		t.Fatal("a new subject has a call link")
	}
	t1 := t0.Add(time.Hour)

	changed, err := s.SetCallLink("  "+callLink+" ", t1)
	if err != nil || !changed || s.CallLink().String() != callLink || s.CallLink().Host() != callLinkHost || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("SetCallLink = %v, %v; link %q at %v", changed, err, s.CallLink().String(), s.UpdatedAt())
	}

	// The same canonical link again changes nothing.
	if changed, err := s.SetCallLink("HTTPS://MEET.example.org/j/1234567890?pwd=FakePwd42", t1.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("same link again = %v, %v", changed, err)
	}

	// A bad link keeps the one that is set.
	if changed, err := s.SetCallLink("javascript:alert(1)", t1.Add(time.Hour)); !errors.Is(err, ErrInvalidCallLink) || changed ||
		s.CallLink().String() != callLink || !s.UpdatedAt().Equal(t1) {
		t.Fatalf("bad link = %v, %v; link %q", changed, err, s.CallLink().String())
	}

	t2 := t1.Add(2 * time.Hour)
	other := "https://bbb.example.edu/b/room"
	if changed, err := s.SetCallLink(other, t2); err != nil || !changed || s.CallLink().String() != other {
		t.Fatalf("another link = %v, %v", changed, err)
	}

	t3 := t2.Add(time.Hour)
	if changed, err := s.ClearCallLink(t3); err != nil || !changed || !s.CallLink().IsZero() || !s.UpdatedAt().Equal(t3) {
		t.Fatalf("ClearCallLink = %v, %v", changed, err)
	}
	if changed, err := s.ClearCallLink(t3.Add(time.Hour)); err != nil || changed || !s.UpdatedAt().Equal(t3) {
		t.Fatalf("ClearCallLink without a link = %v, %v", changed, err)
	}
}

func TestCallLinkOfRemovedSubject(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetCallLink(callLink, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetCallLink("https://other.example.org/", t0.Add(2*time.Hour)); !errors.Is(err, ErrRemoved) || changed {
		t.Fatalf("SetCallLink of a removed subject = %v, %v", changed, err)
	}
	if changed, err := s.ClearCallLink(t0.Add(2 * time.Hour)); !errors.Is(err, ErrRemoved) || changed {
		t.Fatalf("ClearCallLink of a removed subject = %v, %v", changed, err)
	}
	if s.CallLink().String() != callLink {
		t.Fatalf("the removed subject lost its link: %q", s.CallLink().String())
	}
}

func TestRestoreCallLink(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetCallLink(callLink, t0); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	st.Version = 2
	if st.CallLink != callLink {
		t.Fatalf("state link = %q", st.CallLink)
	}
	back, err := Restore(st)
	if err != nil || back.CallLink() != s.CallLink() {
		t.Fatalf("Restore = %v, %v", back, err)
	}

	// A stored value that no longer parses, or is not canonical, is read as no link and never fails the read.
	for _, stored := range []string{"", "javascript:alert(1)", "https://localhost/", " " + callLink, "HTTPS://meet.example.org/"} {
		bad := st
		bad.CallLink = stored
		back, err := Restore(bad)
		if err != nil || !back.CallLink().IsZero() || back.State().CallLink != "" {
			t.Errorf("Restore of %q = %v, %v", stored, back, err)
		}
	}
}

// The subject, its state in slog and the link itself never print the path or the query (a meeting password).
func TestPrintingHidesCallLink(t *testing.T) {
	s := newSubject(t)
	if _, err := s.SetCallLink(callLink, t0); err != nil {
		t.Fatal(err)
	}
	leaks := func(out string) bool {
		return strings.Contains(out, callLinkPwd) || strings.Contains(out, "1234567890")
	}
	for label, v := range map[string]any{"subject": s, "link": s.CallLink(), "subjects": []*Subject{s}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if out := fmt.Sprintf(verb, v); leaks(out) {
				t.Errorf("%s %s: %s", label, verb, out)
			}
		}
	}
	for label, v := range map[string]any{"subject": s, "link": s.CallLink(), "state": s.State()} {
		var text, js bytes.Buffer
		slog.New(slog.NewTextHandler(&text, nil)).Info("x", "v", v)
		slog.New(slog.NewJSONHandler(&js, nil)).Info("x", "v", v)
		if leaks(text.String()) || leaks(js.String()) {
			t.Errorf("%s through slog: %s %s", label, text.String(), js.String())
		}
	}
	if got := fmt.Sprint(s.CallLink()); got != "call link to "+callLinkHost {
		t.Fatalf("link prints %q", got)
	}
	if got := fmt.Sprint(CallLink{}); got != "no call link" {
		t.Fatalf("no link prints %q", got)
	}
}
