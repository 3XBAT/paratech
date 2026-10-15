package testenv

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// AfterData is what the fake SMTP server does once it has received a whole message (the terminating dot).
type AfterData int

// Behaviours after the message.
const (
	// AfterDataReply answers with SMTPBehavior.FinalReply (250 by default).
	AfterDataReply AfterData = iota

	// AfterDataStall never answers: the message is in, but the client waits for the final reply until it gives up.
	// This is the timeout after a possible acceptance of PRD section 14 case 7.
	AfterDataStall

	// AfterDataDrop closes the connection without an answer.
	AfterDataDrop
)

// Drop is a point at which the fake SMTP server closes the connection without a word.
type Drop int

// Drop points.
const (
	DropNone Drop = iota

	// DropAtRcpt closes the connection instead of answering RCPT TO: nothing of the message was sent.
	DropAtRcpt

	// DropAtData closes the connection instead of answering DATA: the client never got to send the content.
	DropAtData

	// DropInContent answers DATA with 354 and closes the connection while the client writes the content.
	DropInContent
)

// SMTPBehavior scripts the fake SMTP server. A reply is a full SMTP reply line such as "550 5.1.1 no such user";
// an empty reply means success.
type SMTPBehavior struct {
	ImplicitTLS bool // TLS from the first byte (port 465 style) instead of STARTTLS
	NoSTARTTLS  bool // do not offer STARTTLS; AUTH is then offered in plaintext, which a client must refuse

	User, Password string // the accepted login; a different one is answered with 535

	StartTLSReply string // reply to STARTTLS instead of the upgrade
	AuthReply     string // reply to AUTH, overriding the login check
	MailReply     string // reply to MAIL FROM
	RcptReply     string // reply to RCPT TO
	DataReply     string // reply to DATA (354 by default)
	FinalReply    string // reply after the message (250 by default)
	AfterData     AfterData
	Drop          Drop
}

// SMTPLogin is one AUTH attempt the fake server saw.
type SMTPLogin struct {
	User, Password string
	TLS            bool // whether the connection was encrypted when the credentials arrived
}

// FakeSMTP is an in-process SMTP server on 127.0.0.1 with a self-signed certificate generated for the test, for the
// end-to-end tests of the smtp adapter (ARCH-61). It speaks enough ESMTP for a real client: EHLO, STARTTLS, AUTH PLAIN,
// MAIL, RCPT, DATA, RSET, NOOP and QUIT. Nothing leaves the machine.
type FakeSMTP struct {
	t     testing.TB
	ln    net.Listener
	tls   *tls.Config
	roots *x509.CertPool

	mu       sync.Mutex
	behavior SMTPBehavior
	messages [][]byte
	logins   []SMTPLogin
	commands []string
	sessions int
	conns    map[net.Conn]bool

	done chan struct{}
	wg   sync.WaitGroup
}

// NewFakeSMTP starts the server; it stops when the test ends.
func NewFakeSMTP(t testing.TB, b SMTPBehavior) *FakeSMTP {
	t.Helper()
	cert, roots := selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake smtp: listen: %v", err)
	}
	f := &FakeSMTP{
		t: t, ln: ln, roots: roots, behavior: b, conns: map[net.Conn]bool{}, done: make(chan struct{}),
		tls: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	}
	f.wg.Add(1)
	go f.serve()
	t.Cleanup(f.close)
	return f
}

// Host returns the address to dial: always 127.0.0.1.
func (f *FakeSMTP) Host() string { return "127.0.0.1" }

// Port returns the listening port.
func (f *FakeSMTP) Port() int { return f.ln.Addr().(*net.TCPAddr).Port }

// RootCAs returns the pool that trusts the server certificate (a test-only CA), for the client's TLS config.
func (f *FakeSMTP) RootCAs() *x509.CertPool { return f.roots }

// SetBehavior changes the script for the next commands.
func (f *FakeSMTP) SetBehavior(b SMTPBehavior) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behavior = b
}

// Messages returns every message the server received completely (up to the terminating dot), whatever it answered:
// for the sender these may all have been accepted.
func (f *FakeSMTP) Messages() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.messages))
	copy(out, f.messages)
	return out
}

// Logins returns the AUTH attempts in order.
func (f *FakeSMTP) Logins() []SMTPLogin {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SMTPLogin(nil), f.logins...)
}

// Commands returns the verbs of every command the server received, in order and upper-cased (for example "EHLO",
// "STARTTLS", "AUTH", "QUIT"); never their arguments, which carry credentials and addresses.
func (f *FakeSMTP) Commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

// Sessions returns how many connections were accepted.
func (f *FakeSMTP) Sessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions
}

func (f *FakeSMTP) script() SMTPBehavior {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.behavior
}

func (f *FakeSMTP) close() {
	close(f.done)
	_ = f.ln.Close()
	f.mu.Lock()
	for c := range f.conns {
		_ = c.Close()
	}
	f.mu.Unlock()
	f.wg.Wait()
}

func (f *FakeSMTP) serve() {
	defer f.wg.Done()
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.sessions++
		f.conns[c] = true
		f.mu.Unlock()
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer func() {
				_ = c.Close()
				f.mu.Lock()
				delete(f.conns, c)
				f.mu.Unlock()
			}()
			f.session(c)
		}()
	}
}

// smtpConn is the state of one session.
type smtpConn struct {
	raw    net.Conn
	conn   net.Conn
	r      *bufio.Reader
	secure bool
}

func (s *smtpConn) reply(line string) error {
	_, err := io.WriteString(s.conn, line+"\r\n")
	return err
}

func (s *smtpConn) readLine() (string, error) {
	line, err := s.r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func (s *smtpConn) upgrade(cfg *tls.Config) error {
	tc := tls.Server(s.raw, cfg)
	if err := tc.Handshake(); err != nil {
		return err
	}
	s.conn, s.r, s.secure = tc, bufio.NewReader(tc), true
	return nil
}

func orDefault(reply, def string) string {
	if reply == "" {
		return def
	}
	return reply
}

func (f *FakeSMTP) session(c net.Conn) {
	s := &smtpConn{raw: c, conn: c, r: bufio.NewReader(c)}
	if f.script().ImplicitTLS {
		if s.upgrade(f.tls) != nil {
			return
		}
	}
	if s.reply("220 fake.smtp.test ESMTP") != nil {
		return
	}
	for {
		line, err := s.readLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		f.mu.Lock()
		f.commands = append(f.commands, verb)
		f.mu.Unlock()
		b := f.script()
		switch verb {
		case "EHLO":
			err = f.ehlo(s, b)
		case "HELO":
			err = s.reply("250 fake.smtp.test")
		case "STARTTLS":
			if b.NoSTARTTLS || s.secure {
				err = s.reply("502 5.5.1 not offered")
				break
			}
			if b.StartTLSReply != "" {
				err = s.reply(b.StartTLSReply)
				break
			}
			if err = s.reply("220 2.0.0 ready"); err == nil {
				err = s.upgrade(f.tls)
			}
		case "AUTH":
			err = f.auth(s, b, arg)
		case "MAIL":
			err = s.reply(orDefault(b.MailReply, "250 2.1.0 ok"))
		case "RCPT":
			if b.Drop == DropAtRcpt {
				return
			}
			err = s.reply(orDefault(b.RcptReply, "250 2.1.5 ok"))
		case "DATA":
			if b.Drop == DropAtData {
				return
			}
			if keep := f.data(s, b); !keep {
				return
			}
		case "RSET", "NOOP":
			err = s.reply("250 2.0.0 ok")
		case "QUIT":
			_ = s.reply("221 2.0.0 bye")
			return
		default:
			err = s.reply("502 5.5.2 unknown command")
		}
		if err != nil {
			return
		}
	}
}

func (f *FakeSMTP) ehlo(s *smtpConn, b SMTPBehavior) error {
	lines := []string{"fake.smtp.test", "8BITMIME", "ENHANCEDSTATUSCODES"}
	if !s.secure && !b.NoSTARTTLS {
		lines = append(lines, "STARTTLS")
	}
	if s.secure || b.NoSTARTTLS {
		lines = append(lines, "AUTH PLAIN")
	}
	for i, l := range lines {
		sep := "-"
		if i == len(lines)-1 {
			sep = " "
		}
		if err := s.reply("250" + sep + l); err != nil {
			return err
		}
	}
	return nil
}

func (f *FakeSMTP) auth(s *smtpConn, b SMTPBehavior, arg string) error {
	mech, initial, _ := strings.Cut(arg, " ")
	if !strings.EqualFold(mech, "PLAIN") {
		return s.reply("504 5.5.4 mechanism not supported")
	}
	if initial == "" {
		if err := s.reply("334 "); err != nil {
			return err
		}
		line, err := s.readLine()
		if err != nil {
			return err
		}
		initial = line
	}
	raw, err := base64.StdEncoding.DecodeString(initial)
	if err != nil {
		return s.reply("501 5.5.2 bad encoding")
	}
	parts := bytes.Split(raw, []byte{0})
	if len(parts) != 3 {
		return s.reply("501 5.5.2 bad PLAIN response")
	}
	login := SMTPLogin{User: string(parts[1]), Password: string(parts[2]), TLS: s.secure}
	f.mu.Lock()
	f.logins = append(f.logins, login)
	f.mu.Unlock()

	switch {
	case b.AuthReply != "":
		return s.reply(b.AuthReply)
	case login.User != b.User || login.Password != b.Password:
		return s.reply("535 5.7.8 authentication failed")
	}
	return s.reply("235 2.7.0 authenticated")
}

// data handles DATA and reports whether the session continues.
func (f *FakeSMTP) data(s *smtpConn, b SMTPBehavior) bool {
	reply := orDefault(b.DataReply, "354 end with <CRLF>.<CRLF>")
	if s.reply(reply) != nil {
		return false
	}
	if !strings.HasPrefix(reply, "354") {
		return true
	}
	if b.Drop == DropInContent {
		return false
	}

	var msg bytes.Buffer
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return false // the client gave up before the end: no message
		}
		if line == ".\r\n" {
			break
		}
		msg.WriteString(strings.TrimPrefix(line, ".")) // undo dot-stuffing
	}
	f.mu.Lock()
	f.messages = append(f.messages, msg.Bytes())
	f.mu.Unlock()

	switch b.AfterData {
	case AfterDataStall:
		// Keep the connection open without a word until the client closes it or the server stops.
		_, _ = io.Copy(io.Discard, s.r)
		return false
	case AfterDataDrop:
		return false
	}
	return s.reply(orDefault(b.FinalReply, "250 2.0.0 queued as fake")) == nil
}

// selfSigned makes a certificate for 127.0.0.1 that is its own CA, and the pool that trusts it.
func selfSigned(t testing.TB) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("fake smtp: key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("fake smtp: serial: %v", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "fake smtp test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true,
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("fake smtp: certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("fake smtp: parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}
