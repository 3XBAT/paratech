package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/hubclient"
	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/testenv"
)

// fixedSettings is a settingsLoader over one aggregate.
type fixedSettings struct {
	s   *settings.Settings
	err error
}

func (f fixedSettings) Load(context.Context) (*settings.Settings, error) { return f.s, f.err }

var bridgeT0 = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func mailConfigured(t *testing.T, security settings.MailSecurity) *settings.Settings {
	t.Helper()
	s := settings.New()
	for _, err := range []error{
		s.SetSMTPHost("smtp.example.org", bridgeT0), s.SetSMTPPort(2525, bridgeT0), s.SetSMTPSecurity(security, bridgeT0),
		s.SetSMTPLogin("student@example.org", bridgeT0), s.SetHomeworkRecipient("teacher@school.example", bridgeT0),
		s.SetHubURL("https://hub.example.com", bridgeT0),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestBridgesMapTheSettings(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		security settings.MailSecurity
		want     smtp.Security
	}{
		{settings.MailSecurityStartTLS, smtp.SecurityStartTLS},
		{settings.MailSecurityTLS, smtp.SecurityImplicitTLS},
	} {
		src := fixedSettings{s: mailConfigured(t, tc.security)}
		acc, err := mailSettings{src}.MailAccount(ctx)
		want := smtp.Account{Host: "smtp.example.org", Port: 2525, Security: tc.want, Username: "student@example.org", From: "student@example.org"}
		if err != nil || acc != want {
			t.Fatalf("MailAccount = %+v, %v; want %+v", acc, err, want)
		}
		if url, err := (hubSettings{src}).HubURL(ctx); err != nil || url != "https://hub.example.com" {
			t.Fatalf("HubURL = %q, %v", url, err)
		}
		if r, err := (recipientSettings{src}).HomeworkRecipient(ctx); err != nil || r.Address() != "teacher@school.example" {
			t.Fatalf("HomeworkRecipient = %q, %v", r.Address(), err)
		}
	}
}

// A delivery keeps the recipient frozen at confirmation: clearing the recipient setting leaves the SMTP account, so a
// pending send still goes out; the proposals see no recipient.
func TestClearedRecipientKeepsTheMailAccount(t *testing.T) {
	ctx := context.Background()
	s := mailConfigured(t, settings.MailSecurityStartTLS)
	if err := s.SetHomeworkRecipient("", bridgeT0); err != nil {
		t.Fatal(err)
	}
	src := fixedSettings{s: s}
	acc, err := mailSettings{src}.MailAccount(ctx)
	want := smtp.Account{Host: "smtp.example.org", Port: 2525, Security: smtp.SecurityStartTLS, Username: "student@example.org", From: "student@example.org"}
	if err != nil || acc != want {
		t.Fatalf("MailAccount = %+v, %v; want %+v", acc, err, want)
	}
	if r, err := (recipientSettings{src}).HomeworkRecipient(ctx); err != nil || !r.IsZero() {
		t.Fatalf("HomeworkRecipient = %q, %v; want none", r.Address(), err)
	}
}

// Not configured is the zero value for every bridge; a failed read is an error.
func TestBridgesWithoutSettings(t *testing.T) {
	ctx := context.Background()
	fresh := fixedSettings{s: settings.New()}
	if acc, err := (mailSettings{fresh}).MailAccount(ctx); err != nil || acc != (smtp.Account{}) {
		t.Fatalf("MailAccount = %+v, %v; want the zero account", acc, err)
	}
	if url, err := (hubSettings{fresh}).HubURL(ctx); err != nil || url != "" {
		t.Fatalf("HubURL = %q, %v", url, err)
	}
	if r, err := (recipientSettings{fresh}).HomeworkRecipient(ctx); err != nil || !r.IsZero() {
		t.Fatalf("HomeworkRecipient = %q, %v", r.Address(), err)
	}

	// A login that is not an address gives no sender: mail is not configured.
	s := mailConfigured(t, settings.MailSecurityStartTLS)
	if err := s.SetSMTPLogin("student", bridgeT0); err != nil {
		t.Fatal(err)
	}
	if acc, err := (mailSettings{fixedSettings{s: s}}).MailAccount(ctx); err != nil || acc != (smtp.Account{}) {
		t.Fatalf("MailAccount with a plain login = %+v, %v", acc, err)
	}

	boom := errors.New("database is busy")
	broken := fixedSettings{err: boom}
	if _, err := (mailSettings{broken}).MailAccount(ctx); !errors.Is(err, boom) {
		t.Fatalf("MailAccount = %v", err)
	}
	if _, err := (hubSettings{broken}).HubURL(ctx); !errors.Is(err, boom) {
		t.Fatalf("HubURL = %v", err)
	}
	if _, err := (recipientSettings{broken}).HomeworkRecipient(ctx); !errors.Is(err, boom) {
		t.Fatalf("HomeworkRecipient = %v", err)
	}
}

// Every Hub URL the settings accept is one the Hub client accepts, so a stored URL never parks the jobs as refused
// (the domain and the adapter keep their own copy of the rule).
func TestStoredHubURLsAreHubClientURLs(t *testing.T) {
	for _, tc := range []struct {
		url      string
		accepted bool // by the settings
	}{
		{"https://hub.example.com", true},
		{"https://hub.example.com/", true},
		{"https://hub.example.com:8443/api/v1", true},
		{"HTTPS://Hub.Example.com", true},
		{"https://hub.example.com/a%20b", true},
		{"https://192.0.2.10", true},
		{"https://[2001:db8::1]:443/", true},
		{"http://localhost:8080", true},
		{"http://LOCALHOST", true},
		{"http://127.0.0.1:9", true},
		{"http://127.0.0.2", true},
		{"http://[::1]:8080/hub", true},
		{"http://hub.example.com", false},
		{"http://127.0.0.1.example.com", false},
		{"https://u:p@hub.example.com", false},
		{"https://hub.example.com/?key=x", false},
		{"https://hub.example.com/#x", false},
		{"https://hub.example.com/?", false},
		{"hub.example.com", false},
		{"//hub.example.com", false},
		{"ftp://hub.example.com", false},
		{"https:hub.example.com", false},
		{"https://", false},
	} {
		inSettings := settings.New().SetHubURL(tc.url, bridgeT0) == nil
		if inSettings != tc.accepted {
			t.Errorf("%q: settings accept %v, want %v", tc.url, inSettings, tc.accepted)
		}
		_, err := hubclient.New(hubclient.Config{BaseURL: tc.url}, testenv.NewMemCredentials())
		if inSettings && err != nil {
			t.Errorf("%q: accepted by the settings, refused by the Hub client: %v", tc.url, err)
		}
	}
}

// Every address the settings accept is a recipient a proposal accepts, so a stored recipient never breaks the
// proposal pipeline (the two contexts keep their own copy of the rule, ARCH-11).
func TestStoredRecipientsAreProposalRecipients(t *testing.T) {
	for _, addr := range []string{
		"teacher@school.example", "a.b+c@d-e.example", "x@y.z", "teacher@localhost", "t@school.example.",
		"T <t@school.example>", "a@b.example,c@d.example", "\"t\"@school.example", "t@[127.0.0.1]", "t@school..example",
		"учитель@school.example", "t@school.example\r\n", strings.Repeat("a", 250) + "@b.cd",
		"a.b.c@school.example", ".a@b.example", "a.@b.example", "a..b@b.example", "@b.example", "a@.b.example",
	} {
		s := settings.New()
		inSettings := s.SetHomeworkRecipient(addr, bridgeT0) == nil
		_, inProposal := proposal.NewRecipient(strings.TrimSpace(addr))
		if inSettings != (inProposal == nil) {
			t.Errorf("%q: settings accept %v, proposals accept %v", addr, inSettings, inProposal == nil)
		}
	}
}
