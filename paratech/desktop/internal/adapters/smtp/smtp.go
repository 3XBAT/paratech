package smtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	netmail "net/mail"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/wneessen/go-mail"

	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/shared"
)

// Security is how the connection to the server is encrypted. Both are TLS; there is no plaintext mode.
type Security string

// Security modes.
const (
	SecurityStartTLS    Security = "starttls" // connect in plaintext, STARTTLS is required before anything else (port 587)
	SecurityImplicitTLS Security = "tls"      // TLS from the first byte (port 465)
)

// Defaults of Config.
const (
	DefaultCredentialName     = "smtp-password"
	DefaultTimeout            = 2 * time.Minute
	DefaultMaxAttachmentBytes = 10 << 20
	DefaultBody               = "Homework attached."
	portStartTLS              = 587
	portImplicitTLS           = 465
)

// Fixed error texts (they are stored as the delivery's last error and logged).
const (
	msgReadAttachment = "smtp: cannot read the attachment"
	msgTooLarge       = "smtp: attachment is larger than the limit"
	msgChanged        = "smtp: attachment is not the confirmed content"
	msgNotConfigured  = "smtp: mail password is not configured"
	msgStoreDown      = "smtp: credential store unavailable"
	msgMessage        = "smtp: cannot build the message"
	msgCancelled      = "smtp: cancelled before sending"
	msgConnect        = "smtp: cannot reach the server"
	msgCertificate    = "smtp: server certificate is not trusted"
	msgSetup          = "smtp: server does not offer the required TLS or authentication"
	msgCheck          = "smtp: connection lost before the message"
	msgWrite          = "smtp: connection lost while sending the message; it may have been accepted"
	msgNoFinalReply   = "smtp: no reply after the message; it may have been accepted"
	msgAmbiguous      = "smtp: unclear result; the message may have been accepted"
	msgReconcile      = "smtp: cannot look a message up over SMTP"
)

// SecretReader is the narrow access of the adapter to the credential store (the wincred adapter in the app).
type SecretReader interface {
	// Get returns the secret under name, or an error wrapping shared.ErrNotFound.
	Get(ctx context.Context, name string) (string, error)
}

// Config is the non-secret mail account setup.
type Config struct {
	Host     string
	Port     int      // 0: 587 for STARTTLS, 465 for implicit TLS
	Security Security // "": STARTTLS
	Username string   // the login; the password is in the credential store
	From     string   // the sender address, a plain local@domain

	CredentialName     string        // "": DefaultCredentialName
	Timeout            time.Duration // whole session; 0: DefaultTimeout
	MaxAttachmentBytes int64         // 0: DefaultMaxAttachmentBytes
	Body               string        // text of the email; "": DefaultBody

	// RootCAs replaces the system roots for verifying the server certificate; nil uses the system roots. Tests pass
	// the CA of the fake server. Verification itself is never disabled.
	RootCAs *x509.CertPool
}

// Mailer sends deliveries over SMTP. Create it with New; it is safe for concurrent use.
type Mailer struct {
	cfg     Config
	secrets SecretReader
}

// New checks the configuration.
func New(cfg Config, secrets SecretReader) (*Mailer, error) {
	if secrets == nil {
		return nil, errors.New("smtp: no credential store")
	}
	if cfg.Security == "" {
		cfg.Security = SecurityStartTLS
	}
	if cfg.Security != SecurityStartTLS && cfg.Security != SecurityImplicitTLS {
		return nil, fmt.Errorf("smtp: unknown security mode %q", cfg.Security)
	}
	if cfg.Port == 0 {
		cfg.Port = map[Security]int{SecurityStartTLS: portStartTLS, SecurityImplicitTLS: portImplicitTLS}[cfg.Security]
	}
	if cfg.CredentialName == "" {
		cfg.CredentialName = DefaultCredentialName
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxAttachmentBytes == 0 {
		cfg.MaxAttachmentBytes = DefaultMaxAttachmentBytes
	}
	if cfg.Body == "" {
		cfg.Body = DefaultBody
	}

	switch {
	case cfg.Host == "" || strings.ContainsFunc(cfg.Host, func(r rune) bool { return r <= ' ' || r == ':' || r == '/' }):
		return nil, errors.New("smtp: host must be a plain host name or IP address")
	case cfg.Port < 1 || cfg.Port > 65535:
		return nil, fmt.Errorf("smtp: port %d out of range", cfg.Port)
	case cfg.Username == "" || strings.ContainsFunc(cfg.Username, isControl):
		return nil, errors.New("smtp: username must be set, without control characters")
	case !plainAddress(cfg.From):
		return nil, errors.New("smtp: sender must be a plain local@domain address")
	case cfg.Timeout < 0 || cfg.MaxAttachmentBytes < 0:
		return nil, errors.New("smtp: timeout and attachment limit must not be negative")
	}
	return &Mailer{cfg: cfg, secrets: secrets}, nil
}

func isControl(r rune) bool { return r < ' ' || r == 0x7f }

// plainAddress reports whether s is exactly one address without a display name or control characters.
func plainAddress(s string) bool {
	a, err := netmail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s && !strings.ContainsFunc(s, isControl)
}

// fail wraps a classified outcome with a fixed text.
func fail(msg string, class error) error { return fmt.Errorf("%s: %w", msg, class) }

// Send emails d with its attachment (senddelivery.EmailAdapter). See the package comment for the classes of errors.
func (m *Mailer) Send(ctx context.Context, d *delivery.Delivery, attachment io.Reader) error {
	if d == nil || attachment == nil {
		return fail(msgMessage, delivery.ErrRejected)
	}
	data, err := io.ReadAll(io.LimitReader(attachment, m.cfg.MaxAttachmentBytes+1))
	if err != nil {
		return fail(msgReadAttachment, delivery.ErrTransient)
	}
	if int64(len(data)) > m.cfg.MaxAttachmentBytes {
		return fail(msgTooLarge, delivery.ErrRejected)
	}
	sum := sha256.Sum256(data)
	if !d.Attachment().Matches(hex.EncodeToString(sum[:]), int64(len(data))) {
		return fail(msgChanged, delivery.ErrAttachmentChanged)
	}

	msg, err := m.message(d, data)
	if err != nil {
		return err
	}

	password, err := m.secrets.Get(ctx, m.cfg.CredentialName)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return fail(msgNotConfigured, delivery.ErrRejected)
	case err != nil:
		return fail(msgStoreDown, delivery.ErrTransient)
	}
	if ctx.Err() != nil {
		return fail(msgCancelled, delivery.ErrTransient)
	}
	return m.send(ctx, msg, password)
}

// message builds the email. The recipient and the subject were validated by the aggregate; they are checked for line
// breaks once more because a header injection here would reach a third party.
func (m *Mailer) message(d *delivery.Delivery, data []byte) (*mail.Msg, error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(d.MessageID(), "<"), ">")
	if !plainAddress(d.Recipient()) || strings.ContainsAny(d.Subject(), "\r\n") || strings.ContainsAny(inner, "\r\n <>") {
		return nil, fail(msgMessage, delivery.ErrRejected)
	}

	msg := mail.NewMsg(mail.WithNoDefaultUserAgent())
	if err := msg.From(m.cfg.From); err != nil {
		return nil, fail(msgMessage, delivery.ErrRejected)
	}
	if err := msg.To(d.Recipient()); err != nil {
		return nil, fail(msgMessage, delivery.ErrRejected)
	}
	msg.Subject(d.Subject())
	msg.SetMessageIDWithValue(inner)
	msg.SetDate()
	msg.SetBodyString(mail.TypeTextPlain, m.cfg.Body)
	if err := msg.AttachReader(path.Base(d.Attachment().Ref().Path()), bytes.NewReader(data),
		mail.WithFileContentType(mail.TypeTextPlain)); err != nil {
		return nil, fail(msgMessage, delivery.ErrRejected)
	}
	return msg, nil
}

// send runs one SMTP session. A cancelled ctx closes the raw connection, which ends a blocked read or write at once.
func (m *Mailer) send(ctx context.Context, msg *mail.Msg, password string) error {
	var (
		mu   sync.Mutex
		conn net.Conn
		stop = func() bool { return true }
	)
	tlsConfig := &tls.Config{ServerName: m.cfg.Host, RootCAs: m.cfg.RootCAs, MinVersion: tls.VersionTLS12}
	dial := func(dctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		raw, err := d.DialContext(dctx, network, addr)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		conn = raw
		stop = context.AfterFunc(ctx, func() { _ = raw.Close() })
		mu.Unlock()
		if m.cfg.Security == SecurityStartTLS {
			return raw, nil
		}
		tc := tls.Client(raw, tlsConfig)
		if err := tc.HandshakeContext(dctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return tc, nil
	}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		stop()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	opts := []mail.Option{
		mail.WithPort(m.cfg.Port), mail.WithTimeout(m.cfg.Timeout), mail.WithTLSConfig(tlsConfig),
		mail.WithDialContextFunc(dial), mail.WithUsername(m.cfg.Username), mail.WithPassword(password),
		// PLAIN and LOGIN are refused by go-mail on a connection that is not TLS; the connection always is here.
		mail.WithOpportunisticSMTPAuth(mail.SMTPAuthSCRAMSHA256, mail.SMTPAuthSCRAMSHA1, mail.SMTPAuthPlain, mail.SMTPAuthLogin),
	}
	if m.cfg.Security == SecurityImplicitTLS {
		opts = append(opts, mail.WithSSL()) // the dialer already did the handshake; go-mail must not ask for STARTTLS
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	client, err := mail.NewClient(m.cfg.Host, opts...)
	if err != nil {
		return fail(msgSetup, delivery.ErrRejected)
	}

	sc, err := client.DialToSMTPClientWithContext(ctx)
	if err != nil {
		return classifySetup(err)
	}
	err = client.SendWithSMTPClient(sc, msg)
	if msg.IsDelivered() {
		// Accepted with 250 after the message; a failure of the RSET or QUIT that follow does not change that.
		_ = client.CloseWithSMTPClient(sc)
		return nil
	}
	_ = client.CloseWithSMTPClient(sc)
	return classifySend(err)
}

// Reconcile cannot find out over SMTP whether a message was accepted, so it always answers delivery.ErrOutcomeUnknown
// (best effort; senddelivery.EmailAdapter.Reconcile).
func (m *Mailer) Reconcile(ctx context.Context, d *delivery.Delivery) (bool, error) {
	return false, fail(msgReconcile, delivery.ErrOutcomeUnknown)
}
