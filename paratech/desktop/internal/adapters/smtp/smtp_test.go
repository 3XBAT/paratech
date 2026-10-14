package smtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
)

const (
	user     = "student@example.org"
	password = "fake-smtp-password-123"
	content  = "# Homework\n\nExercise 3 is done.\n"
	marker   = "SERVERTEXT" // put into server replies; it must never reach an error
)

func newDelivery(t *testing.T, body string) *delivery.Delivery {
	t.Helper()
	ref, err := shared.NewArtifactRef(shared.ArtifactExports, "a1/homework.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	att, err := delivery.NewAttachment(ref, hex.EncodeToString(sum[:]), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	d, err := delivery.New(delivery.Spec{
		ID: "d1", AssignmentID: "a1", Recipient: "teacher@school.example", Subject: "Домашнее задание", Attachment: att,
		MessageID: "<m1.d1@paratech.test>", Key: "proposal:p1", ConfirmedAt: now,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

type fixture struct {
	srv    *testenv.FakeSMTP
	creds  *testenv.MemCredentials
	mailer *Mailer
}

func newFixture(t *testing.T, b testenv.SMTPBehavior, change func(*Config)) *fixture {
	t.Helper()
	if b.User == "" {
		b.User, b.Password = user, password
	}
	srv := testenv.NewFakeSMTP(t, b)
	creds := testenv.NewMemCredentials()
	if err := creds.Set(context.Background(), DefaultCredentialName, password); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Host: srv.Host(), Port: srv.Port(), Username: user, From: "student@example.org", RootCAs: srv.RootCAs(),
		Timeout: 5 * time.Second,
	}
	if b.ImplicitTLS {
		cfg.Security = SecurityImplicitTLS
	}
	if change != nil {
		change(&cfg)
	}
	m, err := New(cfg, creds)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{srv: srv, creds: creds, mailer: m}
}

func (f *fixture) send(t *testing.T, ctx context.Context, body string) error {
	t.Helper()
	return f.mailer.Send(ctx, newDelivery(t, body), strings.NewReader(body))
}

func TestSendOverTLS(t *testing.T) {
	for _, implicit := range []bool{false, true} {
		name := map[bool]string{false: "STARTTLS", true: "implicit TLS"}[implicit]
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, testenv.SMTPBehavior{ImplicitTLS: implicit}, nil)
			if err := f.send(t, context.Background(), content); err != nil {
				t.Fatalf("Send: %v", err)
			}

			msgs := f.srv.Messages()
			if len(msgs) != 1 {
				t.Fatalf("server received %d messages, want 1", len(msgs))
			}
			raw := string(msgs[0])
			for _, want := range []string{
				"Message-ID: <m1.d1@paratech.test>", "To: <teacher@school.example>", "From: <student@example.org>",
				"Subject:\r\n =?UTF-8?q?=D0=94", `filename="homework.md"`, base64.StdEncoding.EncodeToString([]byte(content)),
			} {
				if !strings.Contains(raw, want) {
					t.Errorf("message lacks %q:\n%s", want, raw)
				}
			}
			logins := f.srv.Logins()
			if len(logins) != 1 || logins[0] != (testenv.SMTPLogin{User: user, Password: password, TLS: true}) {
				t.Fatalf("logins %+v, want one over TLS", logins)
			}
		})
	}
}

// Step 3 of the task at the adapter level: the server takes the whole message and then never answers (or drops the
// connection). The adapter cannot know whether it was accepted: ErrOutcomeUnknown, never ErrTransient.
func TestTimeoutAfterTheMessageIsUnknown(t *testing.T) {
	for _, after := range []testenv.AfterData{testenv.AfterDataStall, testenv.AfterDataDrop} {
		t.Run(map[testenv.AfterData]string{testenv.AfterDataStall: "stall", testenv.AfterDataDrop: "drop"}[after], func(t *testing.T) {
			f := newFixture(t, testenv.SMTPBehavior{AfterData: after}, func(c *Config) { c.Timeout = 500 * time.Millisecond })
			err := f.send(t, context.Background(), content)
			if !errors.Is(err, delivery.ErrOutcomeUnknown) || errors.Is(err, delivery.ErrTransient) {
				t.Fatalf("Send = %v, want only ErrOutcomeUnknown", err)
			}
			if len(f.srv.Messages()) != 1 {
				t.Fatalf("server received %d messages", len(f.srv.Messages()))
			}
		})
	}
}

// A job timeout or shutdown cancels the context while the client waits for the final reply: the connection is closed
// at once (not after the SMTP timeout) and the outcome is unknown.
func TestCancelWhileWaitingForTheFinalReply(t *testing.T) {
	f := newFixture(t, testenv.SMTPBehavior{AfterData: testenv.AfterDataStall}, func(c *Config) { c.Timeout = time.Minute })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for len(f.srv.Messages()) == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	err := f.send(t, ctx, content)
	if !errors.Is(err, delivery.ErrOutcomeUnknown) {
		t.Fatalf("Send = %v, want ErrOutcomeUnknown", err)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatal("cancellation did not close the connection")
	}
}

func TestSendErrorClassification(t *testing.T) {
	tests := []struct {
		name     string
		behavior testenv.SMTPBehavior
		change   func(*Config)
		want     error
		messages int
		logins   int
	}{
		{name: "wrong password", behavior: testenv.SMTPBehavior{Password: "other-" + password, User: user},
			want: delivery.ErrRejected, logins: 1},
		{name: "auth temporarily unavailable", behavior: testenv.SMTPBehavior{AuthReply: "454 4.7.0 " + marker},
			want: delivery.ErrTransient, logins: 1},
		{name: "sender refused", behavior: testenv.SMTPBehavior{MailReply: "550 5.7.1 " + marker}, want: delivery.ErrRejected, logins: 1},
		{name: "sender deferred", behavior: testenv.SMTPBehavior{MailReply: "451 4.3.0 " + marker}, want: delivery.ErrTransient, logins: 1},
		{name: "recipient unknown", behavior: testenv.SMTPBehavior{RcptReply: "550 5.1.1 " + marker}, want: delivery.ErrRejected, logins: 1},
		{name: "recipient deferred", behavior: testenv.SMTPBehavior{RcptReply: "450 4.2.1 " + marker}, want: delivery.ErrTransient, logins: 1},
		{name: "data refused", behavior: testenv.SMTPBehavior{DataReply: "554 5.5.1 " + marker}, want: delivery.ErrRejected, logins: 1},
		{name: "message refused after the dot", behavior: testenv.SMTPBehavior{FinalReply: "554 5.7.1 " + marker},
			want: delivery.ErrRejected, messages: 1, logins: 1},
		{name: "message deferred after the dot", behavior: testenv.SMTPBehavior{FinalReply: "451 4.3.0 " + marker},
			want: delivery.ErrTransient, messages: 1, logins: 1},
		// The password is never sent over a plaintext connection, even though the server offers AUTH there.
		{name: "server without STARTTLS", behavior: testenv.SMTPBehavior{NoSTARTTLS: true}, want: delivery.ErrRejected},
		{name: "untrusted certificate", change: func(c *Config) { c.RootCAs = nil }, want: delivery.ErrRejected},
		{name: "STARTTLS server on the implicit TLS setting", change: func(c *Config) { c.Security = SecurityImplicitTLS },
			want: delivery.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.behavior, tt.change)
			err := f.send(t, context.Background(), content)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Send = %v, want %v", err, tt.want)
			}
			if errors.Is(err, delivery.ErrOutcomeUnknown) {
				t.Fatalf("Send = %v: nothing was accepted, the outcome is known", err)
			}
			if got := len(f.srv.Messages()); got != tt.messages {
				t.Fatalf("server received %d messages, want %d", got, tt.messages)
			}
			if got := len(f.srv.Logins()); got != tt.logins {
				t.Fatalf("server saw %d logins, want %d", got, tt.logins)
			}
			for _, l := range f.srv.Logins() {
				if !l.TLS {
					t.Fatal("credentials were sent without TLS")
				}
			}
			assertClean(t, err)
		})
	}
}

func TestConnectionRefusedIsTransient(t *testing.T) {
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
	err = m.Send(context.Background(), newDelivery(t, content), strings.NewReader(content))
	if !errors.Is(err, delivery.ErrTransient) {
		t.Fatalf("Send = %v, want ErrTransient", err)
	}
	assertClean(t, err)
}

// The bytes sent are the bytes checked: a file that changed after the confirmation is not sent at all.
func TestAttachmentIsCheckedBeforeAnythingIsSent(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
		body   string
		want   error
	}{
		{"changed content", nil, content + "edited", delivery.ErrAttachmentChanged},
		{"same size other content", nil, strings.Repeat("x", len(content)), delivery.ErrAttachmentChanged},
		{"too large", func(c *Config) { c.MaxAttachmentBytes = 8 }, content, delivery.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, testenv.SMTPBehavior{}, tt.change)
			err := f.mailer.Send(context.Background(), newDelivery(t, content), strings.NewReader(tt.body))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Send = %v, want %v", err, tt.want)
			}
			if f.srv.Sessions() != 0 {
				t.Fatal("the server was contacted")
			}
		})
	}
}

func TestMissingPasswordIsRejectedWithoutContactingTheServer(t *testing.T) {
	f := newFixture(t, testenv.SMTPBehavior{}, nil)
	if err := f.creds.Delete(context.Background(), DefaultCredentialName); err != nil {
		t.Fatal(err)
	}
	if err := f.send(t, context.Background(), content); !errors.Is(err, delivery.ErrRejected) {
		t.Fatalf("Send = %v, want ErrRejected", err)
	}
	f.creds.Fail(errors.New("vault locked"))
	if err := f.send(t, context.Background(), content); !errors.Is(err, delivery.ErrTransient) {
		t.Fatalf("Send with a failing store = %v, want ErrTransient", err)
	}
	if f.srv.Sessions() != 0 {
		t.Fatal("the server was contacted")
	}
}

func TestCancelledBeforeSendingIsTransient(t *testing.T) {
	f := newFixture(t, testenv.SMTPBehavior{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.send(t, ctx, content); !errors.Is(err, delivery.ErrTransient) {
		t.Fatalf("Send = %v, want ErrTransient", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	valid := Config{Host: "smtp.example.org", Username: user, From: user}
	tests := []struct {
		name   string
		change func(*Config)
		ok     bool
		port   int
	}{
		{"defaults to STARTTLS on 587", func(*Config) {}, true, 587},
		{"implicit TLS on 465", func(c *Config) { c.Security = SecurityImplicitTLS }, true, 465},
		{"unknown security", func(c *Config) { c.Security = "none" }, false, 0},
		{"no host", func(c *Config) { c.Host = "" }, false, 0},
		{"host with port", func(c *Config) { c.Host = "smtp.example.org:25" }, false, 0},
		{"host with line break", func(c *Config) { c.Host = "smtp.example.org\r\n" }, false, 0},
		{"bad port", func(c *Config) { c.Port = 70000 }, false, 0},
		{"no username", func(c *Config) { c.Username = "" }, false, 0},
		{"sender with display name", func(c *Config) { c.From = "Me <" + user + ">" }, false, 0},
		{"sender with line break", func(c *Config) { c.From = user + "\r\nBcc: x@evil.org" }, false, 0},
		{"negative timeout", func(c *Config) { c.Timeout = -1 }, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.change(&cfg)
			m, err := New(cfg, testenv.NewMemCredentials())
			if (err == nil) != tt.ok {
				t.Fatalf("New = %v, want ok %v", err, tt.ok)
			}
			if tt.ok && m.cfg.Port != tt.port {
				t.Fatalf("port %d, want %d", m.cfg.Port, tt.port)
			}
		})
	}
	if _, err := New(valid, nil); err == nil {
		t.Fatal("New without a credential store succeeded")
	}
}

func TestReconcileIsAlwaysUnknown(t *testing.T) {
	f := newFixture(t, testenv.SMTPBehavior{}, nil)
	if ok, err := f.mailer.Reconcile(context.Background(), newDelivery(t, content)); ok || !errors.Is(err, delivery.ErrOutcomeUnknown) {
		t.Fatalf("Reconcile = %v, %v", ok, err)
	}
	if f.srv.Sessions() != 0 {
		t.Fatal("Reconcile contacted the server")
	}
}

// assertClean checks that an error text has none of the secrets or personal data: it is stored and logged.
func assertClean(t *testing.T, err error) {
	t.Helper()
	text := err.Error()
	for _, bad := range []string{password, user, "teacher@school.example", marker, "m1.d1"} {
		if strings.Contains(text, bad) {
			t.Fatalf("error %q contains %q", text, bad)
		}
	}
	if bytes.ContainsAny([]byte(text), "\r\n") {
		t.Fatalf("error %q spans lines", text)
	}
}
