package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/smtp"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/settings/settings"
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

// Every address the settings accept is a recipient a proposal accepts, so a stored recipient never breaks the
// proposal pipeline (the two contexts keep their own copy of the rule, ARCH-11).
func TestStoredRecipientsAreProposalRecipients(t *testing.T) {
	for _, addr := range []string{
		"teacher@school.example", "a.b+c@d-e.example", "x@y.z", "teacher@localhost", "t@school.example.",
		"T <t@school.example>", "a@b.example,c@d.example", "\"t\"@school.example", "t@[127.0.0.1]", "t@school..example",
		"учитель@school.example", "t@school.example\r\n", strings.Repeat("a", 250) + "@b.cd",
	} {
		s := settings.New()
		inSettings := s.SetHomeworkRecipient(addr, bridgeT0) == nil
		_, inProposal := proposal.NewRecipient(strings.TrimSpace(addr))
		if inSettings != (inProposal == nil) {
			t.Errorf("%q: settings accept %v, proposals accept %v", addr, inSettings, inProposal == nil)
		}
	}
}
