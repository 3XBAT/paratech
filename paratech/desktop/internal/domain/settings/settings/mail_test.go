package settings

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// mailSnap is every observable mail and Hub value.
type mailSnap struct {
	recipient, host, login, hub, sender string
	port                                int
	security                            MailSecurity
	configured                          bool
	updatedAt                           time.Time
}

func mailOf(s *Settings) mailSnap {
	return mailSnap{
		recipient: s.HomeworkRecipient(), host: s.SMTPHost(), login: s.SMTPLogin(), hub: s.HubURL(),
		sender: s.MailSender(), port: s.SMTPPort(), security: s.SMTPSecurity(), configured: s.MailConfigured(),
		updatedAt: s.UpdatedAt(),
	}
}

func TestNewHasNoMailOrHub(t *testing.T) {
	want := mailSnap{security: MailSecurityStartTLS}
	if got := mailOf(New()); got != want {
		t.Fatalf("New() mail = %+v, want %+v", got, want)
	}
}

func TestMailSetters(t *testing.T) {
	defaults := mailSnap{security: MailSecurityStartTLS}
	tests := []struct {
		name    string
		apply   func(*Settings) error
		want    mailSnap
		wantErr bool
	}{
		{"recipient", func(s *Settings) error { return s.SetHomeworkRecipient(" teacher@school.example ", t1) },
			mailSnap{recipient: "teacher@school.example", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"recipient with display name", func(s *Settings) error { return s.SetHomeworkRecipient("T <t@school.example>", t1) },
			defaults, true},
		{"two recipients", func(s *Settings) error { return s.SetHomeworkRecipient("a@b.example,c@d.example", t1) }, defaults, true},
		{"recipient with a line break", func(s *Settings) error {
			return s.SetHomeworkRecipient("t@school.example\r\nBcc: x@evil.example", t1)
		}, defaults, true},
		{"recipient without a dotted domain", func(s *Settings) error { return s.SetHomeworkRecipient("t@localhost", t1) }, defaults, true},
		{"non-ASCII recipient", func(s *Settings) error { return s.SetHomeworkRecipient("учитель@school.example", t1) }, defaults, true},
		{"host", func(s *Settings) error { return s.SetSMTPHost("smtp.example.org", t1) },
			mailSnap{host: "smtp.example.org", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"ipv4 host", func(s *Settings) error { return s.SetSMTPHost("127.0.0.1", t1) },
			mailSnap{host: "127.0.0.1", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"host with port", func(s *Settings) error { return s.SetSMTPHost("smtp.example.org:25", t1) }, defaults, true},
		{"host with scheme", func(s *Settings) error { return s.SetSMTPHost("smtps://smtp.example.org", t1) }, defaults, true},
		{"host with a space", func(s *Settings) error { return s.SetSMTPHost("smtp example.org", t1) }, defaults, true},
		{"port", func(s *Settings) error { return s.SetSMTPPort(2525, t1) },
			mailSnap{port: 2525, security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"port out of range", func(s *Settings) error { return s.SetSMTPPort(70000, t1) }, defaults, true},
		{"negative port", func(s *Settings) error { return s.SetSMTPPort(-1, t1) }, defaults, true},
		{"implicit tls", func(s *Settings) error { return s.SetSMTPSecurity(MailSecurityTLS, t1) },
			mailSnap{security: MailSecurityTLS, updatedAt: t1}, false},
		{"plaintext is not a mode", func(s *Settings) error { return s.SetSMTPSecurity("none", t1) }, defaults, true},
		{"login that is an address", func(s *Settings) error { return s.SetSMTPLogin("student@example.org", t1) },
			mailSnap{login: "student@example.org", sender: "student@example.org", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"login that is a name", func(s *Settings) error { return s.SetSMTPLogin("student", t1) },
			mailSnap{login: "student", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"login with a control character", func(s *Settings) error { return s.SetSMTPLogin("stu\x00dent", t1) }, defaults, true},
		{"hub https", func(s *Settings) error { return s.SetHubURL("https://hub.example.com/api", t1) },
			mailSnap{hub: "https://hub.example.com/api", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"hub http loopback", func(s *Settings) error { return s.SetHubURL("http://127.0.0.1:8080", t1) },
			mailSnap{hub: "http://127.0.0.1:8080", security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"hub http remote", func(s *Settings) error { return s.SetHubURL("http://hub.example.com", t1) }, defaults, true},
		{"hub http lookalike", func(s *Settings) error { return s.SetHubURL("http://127.0.0.1.example.com", t1) }, defaults, true},
		{"hub with credentials", func(s *Settings) error { return s.SetHubURL("https://u:p@hub.example.com", t1) }, defaults, true},
		{"hub with a query", func(s *Settings) error { return s.SetHubURL("https://hub.example.com/?key=x", t1) }, defaults, true},
		{"hub relative", func(s *Settings) error { return s.SetHubURL("hub.example.com", t1) }, defaults, true},
		{"hub other scheme", func(s *Settings) error { return s.SetHubURL("ftp://hub.example.com", t1) }, defaults, true},
		{"hub too long", func(s *Settings) error {
			return s.SetHubURL("https://hub.example.com/"+strings.Repeat("a", maxHubURLLen), t1)
		}, defaults, true},
		{"clearing an unset value is a no-op", func(s *Settings) error { return s.SetHubURL("", t1) }, defaults, false},
		{"set and clear", func(s *Settings) error {
			if err := s.SetSMTPHost("smtp.example.org", t0); err != nil {
				return err
			}
			return s.SetSMTPHost("", t1)
		}, mailSnap{security: MailSecurityStartTLS, updatedAt: t1}, false},
		{"missing change time", func(s *Settings) error { return s.SetSMTPHost("smtp.example.org", time.Time{}) }, defaults, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			err := tc.apply(s)
			if tc.wantErr != errors.Is(err, ErrInvalidSettings) || (!tc.wantErr && err != nil) {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if got := mailOf(s); got != tc.want {
				t.Fatalf("mail = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMailConfigured(t *testing.T) {
	full := func(s *Settings) {
		for _, err := range []error{
			s.SetSMTPHost("smtp.example.org", t1), s.SetHomeworkRecipient("teacher@school.example", t1),
			s.SetSMTPLogin("student@example.org", t1),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	tests := []struct {
		name   string
		change func(*Settings) error
		want   bool
	}{
		{"server, recipient and an address login", nil, true},
		{"no server", func(s *Settings) error { return s.SetSMTPHost("", t1) }, false},
		{"no recipient", func(s *Settings) error { return s.SetHomeworkRecipient("", t1) }, false},
		{"no login", func(s *Settings) error { return s.SetSMTPLogin("", t1) }, false},
		{"login is not an address: no sender", func(s *Settings) error { return s.SetSMTPLogin("student", t1) }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			full(s)
			if tc.change != nil {
				if err := tc.change(s); err != nil {
					t.Fatal(err)
				}
			}
			if s.MailConfigured() != tc.want {
				t.Fatalf("MailConfigured = %v, want %v", s.MailConfigured(), tc.want)
			}
		})
	}
}

func TestRestoreMail(t *testing.T) {
	valid := State{
		Version: 2, HomeworkRecipient: "teacher@school.example", SMTPHost: "smtp.example.org", SMTPPort: 465,
		SMTPSecurity: MailSecurityTLS, SMTPLogin: "student@example.org", HubURL: "https://hub.example.com",
	}
	tests := []struct {
		name    string
		mutate  func(*State)
		want    mailSnap
		wantErr bool
	}{
		{name: "valid", want: mailSnap{
			recipient: "teacher@school.example", host: "smtp.example.org", login: "student@example.org",
			hub: "https://hub.example.com", sender: "student@example.org", port: 465, security: MailSecurityTLS, configured: true,
		}},
		{name: "empty security is the default", mutate: func(s *State) { *s = State{Version: 1} },
			want: mailSnap{security: MailSecurityStartTLS}},
		{name: "unknown security", mutate: func(s *State) { s.SMTPSecurity = "none" }, wantErr: true},
		{name: "tampered hub url", mutate: func(s *State) { s.HubURL = "http://evil.example.com" }, wantErr: true},
		{name: "tampered recipient", mutate: func(s *State) { s.HomeworkRecipient = "a@b.example\nBcc: c@d.example" }, wantErr: true},
		{name: "tampered host", mutate: func(s *State) { s.SMTPHost = "smtp.example.org:25" }, wantErr: true},
		{name: "tampered port", mutate: func(s *State) { s.SMTPPort = 70000 }, wantErr: true},
		{name: "tampered login", mutate: func(s *State) { s.SMTPLogin = "a\x00b" }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := valid
			if tc.mutate != nil {
				tc.mutate(&st)
			}
			got, err := Restore(st)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidSettings) || got != nil {
					t.Fatalf("Restore = %v, %v; want ErrInvalidSettings", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m := mailOf(got); m != tc.want {
				t.Fatalf("Restore mail = %+v, want %+v", m, tc.want)
			}
		})
	}
}
