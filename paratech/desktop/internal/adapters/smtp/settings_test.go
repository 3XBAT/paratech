package smtp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/testenv"
)

// accountSetting is an AccountSource the test changes between sends, like the settings in the app.
type accountSetting struct {
	mu  sync.Mutex
	a   Account
	err error
}

func (s *accountSetting) MailAccount(context.Context) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.a, s.err
}

func (s *accountSetting) set(a Account, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.a, s.err = a, err
}

func newSettingsFixture(t *testing.T) (*fixture, *accountSetting) {
	t.Helper()
	srv := testenv.NewFakeSMTP(t, testenv.SMTPBehavior{User: user, Password: password})
	creds := testenv.NewMemCredentials()
	if err := creds.Set(context.Background(), DefaultCredentialName, password); err != nil {
		t.Fatal(err)
	}
	src := &accountSetting{a: Account{Host: srv.Host(), Port: srv.Port(), Username: user, From: user}}
	m, err := New(Config{Accounts: src, RootCAs: srv.RootCAs(), Timeout: 5 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{srv: srv, creds: creds, mailer: m}, src
}

// The account is read at every send: a changed sender applies to the next email without a restart.
func TestAccountFromSettingsAppliesWithoutRestart(t *testing.T) {
	f, src := newSettingsFixture(t)
	if err := f.send(t, context.Background(), content); err != nil {
		t.Fatal(err)
	}
	a, _ := src.MailAccount(context.Background())
	a.From = "other@example.org"
	src.set(a, nil)
	if err := f.send(t, context.Background(), content); err != nil {
		t.Fatal(err)
	}

	msgs := f.srv.Messages()
	if len(msgs) != 2 || !strings.Contains(string(msgs[0]), "From: <"+user+">") ||
		!strings.Contains(string(msgs[1]), "From: <other@example.org>") {
		t.Fatalf("messages do not follow the settings: %d", len(msgs))
	}
}

// Not configured or unusable settings refuse the send for sure (nothing is handed over, the outcome is known); a
// settings read failure is transient.
func TestAccountProblemsSendNothing(t *testing.T) {
	tests := []struct {
		name   string
		change func(a *Account) error
		want   error
		msg    string
	}{
		{"not configured", func(a *Account) error { *a = Account{}; return nil }, delivery.ErrRejected, msgNoAccount},
		{"sender with a display name", func(a *Account) error { a.From = "Me <" + user + ">"; return nil },
			delivery.ErrRejected, msgBadAccount},
		{"host with a port", func(a *Account) error { a.Host += ":25"; return nil }, delivery.ErrRejected, msgBadAccount},
		{"plaintext mode", func(a *Account) error { a.Security = "none"; return nil }, delivery.ErrRejected, msgBadAccount},
		{"settings unreadable", func(*Account) error { return errors.New("database is busy") }, delivery.ErrTransient, msgSettings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, src := newSettingsFixture(t)
			a, _ := src.MailAccount(context.Background())
			err := tt.change(&a)
			src.set(a, err)

			err = f.send(t, context.Background(), content)
			if !errors.Is(err, tt.want) || errors.Is(err, delivery.ErrOutcomeUnknown) || !strings.HasPrefix(err.Error(), tt.msg) {
				t.Fatalf("Send = %v, want %v with %q", err, tt.want, tt.msg)
			}
			if f.srv.Sessions() != 0 {
				t.Fatal("the server was contacted")
			}
			assertClean(t, err)
		})
	}
}

// Every address the settings accept, as the recipient or as the sender (the login), passes the address check of the
// adapter, so a stored setting never makes a confirmed email fail here.
func TestSettingsAddressesArePlainAddresses(t *testing.T) {
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		addr     string
		accepted bool // by the settings
	}{
		{"teacher@school.example", true},
		{"a.b+hw@mail.school-1.example", true},
		{"x@y.z", true},
		{"a!#$%&'*+-/=?^_`{|}~b@school.example", true},
		{"a.b.c@d.e.example", true},
		{".a@b.example", false},
		{"a.@b.example", false},
		{"a..b@b.example", false},
		{"a@b..example", false},
		{"a@.b.example", false},
		{"a@b.example.", false},
		{"@b.example", false},
		{"\"a b\"@b.example", false},
		{"a@[127.0.0.1]", false},
		{"T <t@b.example>", false},
		{"a@b.example,c@d.example", false},
	} {
		s := settings.New()
		recipient := s.SetHomeworkRecipient(tc.addr, now) == nil
		sender := s.SetSMTPLogin(tc.addr, now) == nil && s.MailSender() == tc.addr
		if recipient != tc.accepted || sender != tc.accepted {
			t.Errorf("%q: settings accept it as recipient %v, as sender %v; want %v", tc.addr, recipient, sender, tc.accepted)
		}
		if (recipient || sender) && !plainAddress(tc.addr) {
			t.Errorf("%q: accepted by the settings, refused by the adapter", tc.addr)
		}
	}
}

func TestNewTakesOneAccount(t *testing.T) {
	_, err := New(Config{Host: "smtp.example.org", Username: user, From: user, Accounts: &accountSetting{}}, testenv.NewMemCredentials())
	if err == nil {
		t.Fatal("New with a fixed account and a source succeeded")
	}
	if _, err := New(Config{Accounts: &accountSetting{}}, testenv.NewMemCredentials()); err != nil {
		t.Fatalf("a source without an account yet: %v", err)
	}
}
