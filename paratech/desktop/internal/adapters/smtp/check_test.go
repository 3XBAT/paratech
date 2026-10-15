package smtp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

// A check runs the session setup of a send (EHLO, STARTTLS, AUTH) and QUIT, and nothing of a message.
func TestCheckOpensASessionWithoutAMessage(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		name := map[bool]string{false: "STARTTLS", true: "implicit TLS"}[implicit]
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, testenv.SMTPBehavior{ImplicitTLS: implicit}, nil)
			if err := f.mailer.Check(context.Background()); err != nil {
				t.Fatalf("Check: %v", err)
			}

			want := []string{"EHLO", "STARTTLS", "EHLO", "AUTH", "QUIT"}
			if implicit {
				want = []string{"EHLO", "AUTH", "QUIT"}
			}
			if got := f.srv.Commands(); !slices.Equal(got, want) {
				t.Fatalf("commands %v, want %v", got, want)
			}
			if len(f.srv.Messages()) != 0 {
				t.Fatal("a check delivered a message")
			}
			logins := f.srv.Logins()
			if len(logins) != 1 || logins[0] != (testenv.SMTPLogin{User: user, Password: password, TLS: true}) {
				t.Fatalf("logins %+v, want one over TLS", logins)
			}
		})
	}
}

// The setup failures are told apart by class; the delivery class of Send is unchanged. The reply to AUTH is recognized
// by go-mail's error (goMailAuthFailed): a 5xx to STARTTLS is not a rejected login.
func TestCheckClassifiesTheSetup(t *testing.T) {
	tests := []struct {
		name     string
		behavior testenv.SMTPBehavior
		change   func(*Config)
		kind     error
		class    error
		logins   int
	}{
		{name: "wrong password", behavior: testenv.SMTPBehavior{User: user, Password: "other-" + password},
			kind: ErrLoginRejected, class: delivery.ErrRejected, logins: 1},
		{name: "login refused by policy", behavior: testenv.SMTPBehavior{AuthReply: "534 5.7.9 " + marker},
			kind: ErrLoginRejected, class: delivery.ErrRejected, logins: 1},
		{name: "login temporarily unavailable", behavior: testenv.SMTPBehavior{AuthReply: "454 4.7.0 " + marker},
			kind: ErrServerTemporary, class: delivery.ErrTransient, logins: 1},
		{name: "STARTTLS refused", behavior: testenv.SMTPBehavior{StartTLSReply: "554 5.7.0 " + marker},
			kind: ErrServerRefused, class: delivery.ErrRejected},
		{name: "STARTTLS deferred", behavior: testenv.SMTPBehavior{StartTLSReply: "454 4.7.0 " + marker},
			kind: ErrServerTemporary, class: delivery.ErrTransient},
		{name: "no STARTTLS offered", behavior: testenv.SMTPBehavior{NoSTARTTLS: true},
			kind: ErrSetupUnsupported, class: delivery.ErrRejected},
		{name: "untrusted certificate", change: func(c *Config) { c.RootCAs = nil },
			kind: ErrCertificate, class: delivery.ErrRejected},
		{name: "implicit TLS against a STARTTLS server", change: func(c *Config) { c.Security = SecurityImplicitTLS },
			kind: ErrSetupUnsupported, class: delivery.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.behavior, tt.change)
			err := f.mailer.Check(context.Background())
			if !errors.Is(err, tt.kind) || !errors.Is(err, tt.class) {
				t.Fatalf("Check = %v, want %v and %v", err, tt.kind, tt.class)
			}
			for _, other := range sessionKinds {
				if other != tt.kind && errors.Is(err, other) {
					t.Fatalf("Check = %v is also %v", err, other)
				}
			}
			if got := len(f.srv.Logins()); got != tt.logins {
				t.Fatalf("server saw %d logins, want %d", got, tt.logins)
			}
			for _, l := range f.srv.Logins() {
				if !l.TLS {
					t.Fatal("credentials were sent without TLS")
				}
			}
			if slices.ContainsFunc(f.srv.Commands(), func(c string) bool { return c == "MAIL" || c == "RCPT" || c == "DATA" }) {
				t.Fatalf("a check sent a transaction: %v", f.srv.Commands())
			}
			assertCheckClean(t, err, f.srv.Host())

			// Send stops at the same step with the same class.
			if err := f.send(t, context.Background(), content); !errors.Is(err, tt.kind) || !errors.Is(err, tt.class) {
				t.Fatalf("Send = %v, want %v and %v", err, tt.kind, tt.class)
			}
		})
	}
}

func TestCheckUnreachableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	creds := testenv.NewMemCredentials()
	_ = creds.Set(context.Background(), DefaultCredentialName, password)
	m, err := New(Config{Host: "127.0.0.1", Port: port, Username: user, From: user, Timeout: 2 * time.Second}, creds)
	if err != nil {
		t.Fatal(err)
	}
	err = m.Check(context.Background())
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, delivery.ErrTransient) {
		t.Fatalf("Check = %v, want ErrUnreachable", err)
	}
	assertCheckClean(t, err, "127.0.0.1")
}

// A server that accepts the connection and never greets is cut off by the caller's deadline, not the session timeout.
func TestCheckEndsAtTheContextDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()

	creds := testenv.NewMemCredentials()
	_ = creds.Set(context.Background(), DefaultCredentialName, password)
	port := ln.Addr().(*net.TCPAddr).Port
	m, err := New(Config{Host: "127.0.0.1", Port: port, Username: user, From: user, Timeout: time.Minute}, creds)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if err := m.Check(ctx); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Check = %v, want ErrUnreachable", err)
	}
	if time.Since(begin) > 20*time.Second {
		t.Fatal("the deadline did not end the check")
	}
}

// Without an account or a password the server is never contacted.
func TestCheckPreconditions(t *testing.T) {
	unsupported := fmt.Errorf("wincred: only on Windows: %w", errors.ErrUnsupported)
	tests := []struct {
		name    string
		account func(*Account) error
		store   func(*testenv.MemCredentials)
		kind    error
	}{
		{name: "not configured", account: func(a *Account) error { *a = Account{}; return nil }, kind: ErrNotConfigured},
		{name: "invalid account", account: func(a *Account) error { a.Host += ":25"; return nil }, kind: ErrAccountInvalid},
		{name: "settings unreadable", account: func(*Account) error { return errors.New("database is busy") }, kind: ErrSettingsUnavailable},
		{name: "no password", store: func(c *testenv.MemCredentials) { _ = c.Delete(context.Background(), DefaultCredentialName) },
			kind: ErrNoPassword},
		{name: "store failing", store: func(c *testenv.MemCredentials) { c.Fail(fmt.Errorf("%w: locked", shared.ErrStorage)) },
			kind: ErrStoreUnavailable},
		{name: "store unsupported", store: func(c *testenv.MemCredentials) { c.Fail(unsupported) }, kind: errors.ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, src := newSettingsFixture(t)
			if tt.account != nil {
				a, _ := src.MailAccount(context.Background())
				err := tt.account(&a)
				src.set(a, err)
			}
			if tt.store != nil {
				tt.store(f.creds)
			}
			err := f.mailer.Check(context.Background())
			if !errors.Is(err, tt.kind) {
				t.Fatalf("Check = %v, want %v", err, tt.kind)
			}
			if f.srv.Sessions() != 0 {
				t.Fatal("the server was contacted")
			}
			assertCheckClean(t, err, f.srv.Host())
		})
	}
}

var sessionKinds = []error{
	ErrNotConfigured, ErrAccountInvalid, ErrSettingsUnavailable, ErrNoPassword, ErrStoreUnavailable, ErrUnreachable,
	ErrCertificate, ErrSetupUnsupported, ErrLoginRejected, ErrServerTemporary, ErrServerRefused,
}

// assertCheckClean is assertClean plus the server's host: the result of a check is shown and logged.
func assertCheckClean(t *testing.T, err error, host string) {
	t.Helper()
	assertClean(t, err)
	if text := err.Error(); strings.Contains(text, host) || strings.Contains(text, "fake.smtp.test") {
		t.Fatalf("error %q names the server", text)
	}
}
