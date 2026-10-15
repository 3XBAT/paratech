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
	gosmtp "github.com/wneessen/go-mail/smtp"

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
	msgNoAccount      = "smtp: mail is not configured"
	msgSettings       = "smtp: mail settings cannot be read"
	msgBadAccount     = "smtp: mail settings are invalid"
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

// Classes of a failed session setup and of its preconditions, for errors.Is next to the delivery outcome class. They do
// not change the fixed texts; Check's caller tells the user which one it was.
var (
	ErrNotConfigured       = errors.New("smtp: account not configured")    // no account in the settings
	ErrAccountInvalid      = errors.New("smtp: account invalid")           // the account breaks the adapter's rules
	ErrSettingsUnavailable = errors.New("smtp: settings unavailable")      // the account could not be read
	ErrNoPassword          = errors.New("smtp: no password stored")        // nothing under the credential name
	ErrStoreUnavailable    = errors.New("smtp: credential store failed")   // also errors.ErrUnsupported off Windows
	ErrUnreachable         = errors.New("smtp: server unreachable")        // network, timeout, cancellation
	ErrCertificate         = errors.New("smtp: certificate not trusted")   // TLS verification failed
	ErrSetupUnsupported    = errors.New("smtp: TLS or login not offered")  // no STARTTLS, no usable AUTH, TLS failure
	ErrLoginRejected       = errors.New("smtp: login rejected")            // a 5xx reply to AUTH
	ErrServerTemporary     = errors.New("smtp: server temporarily failed") // a 4xx reply during the setup
	ErrServerRefused       = errors.New("smtp: server refused")            // another reply during the setup
)

// classed adds session classes to an error without changing its text.
type classed struct {
	err   error
	kinds []error
}

func (e *classed) Error() string   { return e.err.Error() }
func (e *classed) Unwrap() []error { return append([]error{e.err}, e.kinds...) }

func withKind(err error, kinds ...error) error { return &classed{err: err, kinds: kinds} }

// SecretReader is the narrow access of the adapter to the credential store (the wincred adapter in the app).
type SecretReader interface {
	// Get returns the secret under name, or an error wrapping shared.ErrNotFound.
	Get(ctx context.Context, name string) (string, error)
}

// Account is the non-secret mail account.
type Account struct {
	Host     string
	Port     int      // 0: 587 for STARTTLS, 465 for implicit TLS
	Security Security // "": STARTTLS
	Username string   // the login; the password is in the credential store
	From     string   // the sender address, a plain local@domain
}

// AccountSource gives the account at send time (the local settings in the app), so a change applies to the next send
// without a restart.
type AccountSource interface {
	// MailAccount returns the account; the zero Account means mail is not configured.
	MailAccount(ctx context.Context) (Account, error)
}

// Config is the non-secret mail setup: a fixed account (Host to From), or Accounts read at every send (exactly one).
type Config struct {
	Host     string
	Port     int      // 0: 587 for STARTTLS, 465 for implicit TLS
	Security Security // "": STARTTLS
	Username string   // the login; the password is in the credential store
	From     string   // the sender address, a plain local@domain

	Accounts AccountSource

	// Timeout is the I/O deadline of the session; 0: DefaultTimeout. go-mail sets it when it dials and resets it once
	// before the message, so it bounds the setup (connect, TLS, EHLO, AUTH) and then MAIL through the final reply
	// separately: a session may take up to twice as long. The job Timeout must be at least 2x this value.
	Timeout time.Duration

	CredentialName     string // "": DefaultCredentialName
	MaxAttachmentBytes int64  // 0: DefaultMaxAttachmentBytes
	Body               string // text of the email; "": DefaultBody

	// RootCAs replaces the system roots for verifying the server certificate; nil uses the system roots. Tests pass
	// the CA of the fake server. Verification itself is never disabled.
	RootCAs *x509.CertPool
}

// Mailer sends deliveries over SMTP. Create it with New; it is safe for concurrent use.
type Mailer struct {
	cfg      Config
	accounts AccountSource
	secrets  SecretReader
}

// fixedAccount is the AccountSource of the fixed account of Config.
type fixedAccount Account

func (a fixedAccount) MailAccount(context.Context) (Account, error) { return Account(a), nil }

// New checks the configuration.
func New(cfg Config, secrets SecretReader) (*Mailer, error) {
	if secrets == nil {
		return nil, errors.New("smtp: no credential store")
	}
	fixed := Account{Host: cfg.Host, Port: cfg.Port, Security: cfg.Security, Username: cfg.Username, From: cfg.From}
	accounts := cfg.Accounts
	switch {
	case accounts != nil && fixed != (Account{}):
		return nil, errors.New("smtp: both a fixed account and an account source")
	case accounts == nil:
		a, err := checkAccount(fixed)
		if err != nil {
			return nil, err
		}
		accounts = fixedAccount(a)
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

	if cfg.Timeout < 0 || cfg.MaxAttachmentBytes < 0 {
		return nil, errors.New("smtp: timeout and attachment limit must not be negative")
	}
	return &Mailer{cfg: cfg, accounts: accounts, secrets: secrets}, nil
}

// checkAccount applies the defaults of the port and the security mode and checks the account.
func checkAccount(a Account) (Account, error) {
	if a.Security == "" {
		a.Security = SecurityStartTLS
	}
	if a.Security != SecurityStartTLS && a.Security != SecurityImplicitTLS {
		return Account{}, fmt.Errorf("smtp: unknown security mode %q", a.Security)
	}
	if a.Port == 0 {
		a.Port = map[Security]int{SecurityStartTLS: portStartTLS, SecurityImplicitTLS: portImplicitTLS}[a.Security]
	}
	switch {
	case a.Host == "" || strings.ContainsFunc(a.Host, func(r rune) bool { return r <= ' ' || r == ':' || r == '/' }):
		return Account{}, errors.New("smtp: host must be a plain host name or IP address")
	case a.Port < 1 || a.Port > 65535:
		return Account{}, fmt.Errorf("smtp: port %d out of range", a.Port)
	case a.Username == "" || strings.ContainsFunc(a.Username, isControl):
		return Account{}, errors.New("smtp: username must be set, without control characters")
	case !plainAddress(a.From):
		return Account{}, errors.New("smtp: sender must be a plain local@domain address")
	}
	return a, nil
}

// account reads the account of one send. Nothing configured, or an account that breaks the rules, is a definite
// refusal: nothing is handed over.
func (m *Mailer) account(ctx context.Context) (Account, error) {
	a, err := m.accounts.MailAccount(ctx)
	switch {
	case err != nil:
		return Account{}, withKind(fail(msgSettings, delivery.ErrTransient), ErrSettingsUnavailable)
	case a == Account{}:
		return Account{}, withKind(fail(msgNoAccount, delivery.ErrRejected), ErrNotConfigured)
	}
	if a, err = checkAccount(a); err != nil {
		return Account{}, withKind(fail(msgBadAccount, delivery.ErrRejected), ErrAccountInvalid)
	}
	return a, nil
}

// password reads the SMTP password of one session. A missing one is a definite refusal; a failing store is transient.
func (m *Mailer) password(ctx context.Context) (string, error) {
	password, err := m.secrets.Get(ctx, m.cfg.CredentialName)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "", withKind(fail(msgNotConfigured, delivery.ErrRejected), ErrNoPassword)
	case errors.Is(err, errors.ErrUnsupported):
		return "", withKind(fail(msgStoreDown, delivery.ErrTransient), ErrStoreUnavailable, errors.ErrUnsupported)
	case err != nil:
		return "", withKind(fail(msgStoreDown, delivery.ErrTransient), ErrStoreUnavailable)
	}
	return password, nil
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

	acc, err := m.account(ctx)
	if err != nil {
		return err
	}
	msg, err := m.message(d, data, acc.From)
	if err != nil {
		return err
	}

	password, err := m.password(ctx)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fail(msgCancelled, delivery.ErrTransient)
	}
	return m.session(ctx, acc, password, func(client *mail.Client, sc *gosmtp.Client) error {
		err := client.SendWithSMTPClient(sc, msg)
		delivered := msg.IsDelivered()
		_ = client.CloseWithSMTPClient(sc)
		if delivered {
			// Accepted with 250 after the message; a failure of the RSET or QUIT that follow does not change that.
			return nil
		}
		return classifySend(err)
	})
}

// Check tests the account without sending anything: it opens the session exactly as Send does (the account from the
// settings, the password from the store, the same TLS and authentication) and ends it with QUIT right after AUTH. No
// MAIL, RCPT or DATA is sent. Errors have Send's fixed texts; errors.Is with the session classes (ErrNotConfigured ...
// ErrServerRefused) tells what failed.
func (m *Mailer) Check(ctx context.Context) error {
	acc, err := m.account(ctx)
	if err != nil {
		return err
	}
	password, err := m.password(ctx)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fail(msgCancelled, delivery.ErrTransient)
	}
	return m.session(ctx, acc, password, func(client *mail.Client, sc *gosmtp.Client) error {
		_ = client.CloseWithSMTPClient(sc) // the login worked; a lost QUIT does not change that
		return nil
	})
}

// message builds the email. The recipient and the subject were validated by the aggregate; they are checked for line
// breaks once more because a header injection here would reach a third party.
func (m *Mailer) message(d *delivery.Delivery, data []byte, from string) (*mail.Msg, error) {
	inner := strings.TrimSuffix(strings.TrimPrefix(d.MessageID(), "<"), ">")
	if !plainAddress(d.Recipient()) || strings.ContainsAny(d.Subject(), "\r\n") || strings.ContainsAny(inner, "\r\n <>") {
		return nil, fail(msgMessage, delivery.ErrRejected)
	}

	msg := mail.NewMsg(mail.WithNoDefaultUserAgent())
	if err := msg.From(from); err != nil {
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

// session opens one SMTP session (connect, TLS, EHLO, AUTH), hands it to use and releases the connection afterwards.
// Send and Check share it, so a check proves exactly the setup a send uses. A cancelled ctx closes the raw connection,
// which ends a blocked read or write at once.
func (m *Mailer) session(ctx context.Context, acc Account, password string, use func(*mail.Client, *gosmtp.Client) error) error {
	var (
		mu   sync.Mutex
		conn net.Conn
		stop = func() bool { return true }
	)
	tlsConfig := &tls.Config{ServerName: acc.Host, RootCAs: m.cfg.RootCAs, MinVersion: tls.VersionTLS12}
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
		if acc.Security == SecurityStartTLS {
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
		mail.WithPort(acc.Port), mail.WithTimeout(m.cfg.Timeout), mail.WithTLSConfig(tlsConfig),
		mail.WithDialContextFunc(dial), mail.WithUsername(acc.Username), mail.WithPassword(password),
		// PLAIN and LOGIN are refused by go-mail on a connection that is not TLS; the connection always is here.
		mail.WithOpportunisticSMTPAuth(mail.SMTPAuthSCRAMSHA256, mail.SMTPAuthSCRAMSHA1, mail.SMTPAuthPlain, mail.SMTPAuthLogin),
	}
	if acc.Security == SecurityImplicitTLS {
		opts = append(opts, mail.WithSSL()) // the dialer already did the handshake; go-mail must not ask for STARTTLS
	} else {
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	client, err := mail.NewClient(acc.Host, opts...)
	if err != nil {
		return withKind(fail(msgSetup, delivery.ErrRejected), ErrSetupUnsupported)
	}

	sc, err := client.DialToSMTPClientWithContext(ctx)
	if err != nil {
		return classifySetup(err)
	}
	return use(client, sc)
}

// Reconcile cannot find out over SMTP whether a message was accepted, so it always answers delivery.ErrOutcomeUnknown
// (best effort; senddelivery.EmailAdapter.Reconcile).
func (m *Mailer) Reconcile(ctx context.Context, d *delivery.Delivery) (bool, error) {
	return false, fail(msgReconcile, delivery.ErrOutcomeUnknown)
}
