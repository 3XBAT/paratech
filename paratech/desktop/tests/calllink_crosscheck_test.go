package tests

// Cross-check of the two copies of the call link rule (ARCH-11, ADR-0032, TASK-364): the library checks the link the
// student keeps on a subject (subject.ParseCallLink), the schedule owns the rule the opener re-checks
// (schedule/values.ParseCallLink, adapters/weblink). They must agree on every input that carries a scheme and give the
// same canonical form and host: a link the library stores but the opener refuses would be a «Подключиться» that does
// nothing. Text without a scheme the library reads as the same text with "https://" in front.

import (
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/subject"
	schedvalues "paratech/desktop/internal/domain/schedule/values"
)

func TestSubjectAndScheduleCallLinksAgree(t *testing.T) {
	corpus := []string{
		"https://meet.example.org/j/1234567890?pwd=FakePwd42", "  HTTPS://Meet.Example.ORG/j/1?pwd=AbC  ",
		"http://bbb.university.example.edu/b/abc-def", "https://a.example.org", "https://a.example.org?x=1",
		"https://teams.example.com/l/meetup-join/19%3ameeting_x%40thread.v2/0?context=%7b%22Tid%22%7d",
		"https://a-b.example.org/!$&()*+,;=:@/~._-", "https://a.example.org/%41%4a", "https://a.b.c.d.example.org/",
		"https://" + strings.Repeat("a", 63) + ".example.org/", "https://a.example.org/" + strings.Repeat("x", 2026),
		"https://a.example.org/" + strings.Repeat("x", 2027), "https://a.example.org/" + strings.Repeat("x", 3000),
		"", "   ", "https://a.example.org/a b", "https://a.example.org/a\nb", "https://a.example.org/путь",
		`https://a.example.org/"x"`, "https://a.example.org/'x'", "https://a.example.org/<x>", `https://a.example.org\x`,
		"https://a.example.org/^", "https://a.example.org/{x}", "https://a.example.org/|", "https://a.example.org/`x`",
		"https://a.example.org/x#y", "javascript://a.example.org/%0aalert(1)", "javascript:alert(1)",
		"mailto:a@example.org", "file:///c:/x", "https:/a.example.org/",
		"file://a.example.org/c$/x", "ftp://a.example.org/x", "https:///x", "https://user:pw@a.example.org/",
		"https://a.example.org:8443/", "https://[::1]/", "https://127.0.0.1/", "https://a.0x7f/", "https://a.0x/",
		"https://a.123/", "https://localhost/", "https://a..example.org/", "https://a.example.org./",
		"https://-a.example.org/", "https://a-.example.org/", "https://xn--e1afmkfd.example.org/",
		"https://a_b.example.org/", "https://a%2eexample.org/", "https://" + strings.Repeat("a", 64) + ".example.org/",
		"https://" + strings.Repeat("a.", 130) + "org/", "https://a.example.org/%zz", "https://a.example.org/%4",
		"https://a.example.org/[x]", "https://a.example.org/%", "Https://A.Example.Org/Path?Q=Mixed",
		"https://a.example.org/x?y=%E2%82%AC", "https://а.example.org/", "https://a.example.org/\x00",
		"https://a.example.org/\x7f", "http//a.example.org/", "https:/a.example.org/", "https://a.example.org//x",
	}
	for _, in := range corpus {
		lib, libErr := subject.ParseCallLink(in)
		sched, schedErr := schedvalues.ParseCallLink(in)
		if (libErr == nil) != (schedErr == nil) {
			t.Errorf("%q: library accepts %v, schedule accepts %v", in, libErr == nil, schedErr == nil)
			continue
		}
		if lib.String() != sched.String() || lib.Host() != sched.Host() {
			t.Errorf("%q: library %q %q, schedule %q %q", in, lib.String(), lib.Host(), sched.String(), sched.Host())
		}
	}

	// Text without a scheme: the library adds "https://" (owner 2026-11-03), the schedule rule does not, so the
	// library's answer must be the schedule's answer on the prefixed text, and what it stores must pass the schedule.
	schemeless := []string{
		"meet.example.org/j/1", "example.com", "  Meet.Example.ORG/j/1?pwd=AbC ", "a.example.org?x=1",
		"a.example.org/r?u=https://b.example.org/", "a.example.org:8443/x", "localhost:3000", "user@a.example.org/",
		"//a.example.org/x", "http//a.example.org/", "127.0.0.1/x", "a.example.org/" + strings.Repeat("x", 2026),
		"a.example.org/" + strings.Repeat("x", 2027),
	}
	for _, in := range schemeless {
		lib, libErr := subject.ParseCallLink(in)
		sched, schedErr := schedvalues.ParseCallLink("https://" + strings.TrimSpace(in))
		if (libErr == nil) != (schedErr == nil) || lib.String() != sched.String() || lib.Host() != sched.Host() {
			t.Errorf("%q: library %q %v, schedule on https %q %v", in, lib.String(), libErr, sched.String(), schedErr)
			continue
		}
		if libErr == nil {
			if again, err := schedvalues.ParseCallLink(lib.String()); err != nil || again.String() != lib.String() {
				t.Errorf("%q: the stored form fails the schedule rule: %v", in, err)
			}
		}
	}

	if subject.MaxCallLinkBytes != schedvalues.MaxCallLinkBytes {
		t.Fatalf("limits differ: %d and %d", subject.MaxCallLinkBytes, schedvalues.MaxCallLinkBytes)
	}
}
