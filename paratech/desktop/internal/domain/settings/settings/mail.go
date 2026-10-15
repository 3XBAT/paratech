package settings

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
)

// MailSecurity is how the connection to the SMTP server is encrypted. Both modes are TLS; there is no plaintext mode.
type MailSecurity string

// Mail security modes.
const (
	MailSecurityStartTLS MailSecurity = "starttls" // STARTTLS is required before anything else (port 587 by default)
	MailSecurityTLS      MailSecurity = "tls"      // TLS from the first byte (port 465 by default)
)

// DefaultMailSecurity is the mode of a fresh installation.
const DefaultMailSecurity = MailSecurityStartTLS

// Valid reports whether m is a known mode.
func (m MailSecurity) Valid() bool { return m == MailSecurityStartTLS || m == MailSecurityTLS }

// Bounds of the mail and Hub settings.
const (
	maxAddressLen = 254
	maxHostLen    = 253
	maxLoginLen   = 254
	maxPort       = 65535
	maxHubURLLen  = 2048
)

// checkAddress accepts a plain address "local@domain": printable ASCII without spaces, no display name and none of
// the characters that would make it several addresses or a header (< > ( ) , ; : " [ ] \). Both parts are dot-atoms
// (no leading, trailing or double dot), as net/mail requires. It is the rule of the homework recipient of the
// proposals (coursework), so every stored address is one a proposal accepts.
func checkAddress(s string) error {
	if len(s) < 3 || len(s) > maxAddressLen {
		return fmt.Errorf("%w: address length must be 3-%d", ErrInvalidSettings, maxAddressLen)
	}
	if strings.ContainsFunc(s, func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsPrint(r) || unicode.IsSpace(r) || strings.ContainsRune(`<>(),;:"[]\`, r)
	}) {
		return fmt.Errorf("%w: address has a forbidden character", ErrInvalidSettings)
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || strings.Contains(domain, "@") {
		return fmt.Errorf("%w: address needs exactly one @", ErrInvalidSettings)
	}
	if !dotAtom(local) {
		return fmt.Errorf("%w: address has a malformed local part", ErrInvalidSettings)
	}
	if !strings.Contains(domain, ".") || !dotAtom(domain) {
		return fmt.Errorf("%w: address has a malformed domain", ErrInvalidSettings)
	}
	return nil
}

// dotAtom reports whether s is non-empty and has no leading, trailing or double dot.
func dotAtom(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".") && !strings.Contains(s, "..")
}

func isAddress(s string) bool { return checkAddress(s) == nil }

// Bounds of a host name label (RFC 1035).
const maxLabelLen = 63

// checkHost accepts a plain host name or IPv4 address: dot-separated labels of 1-63 ASCII letters, digits and
// hyphens, none starting or ending with a hyphen, and no trailing dot. No port, scheme or path: the port and the
// security mode are separate settings.
func checkHost(s string) error {
	ok := s != "" && len(s) <= maxHostLen
	for label := range strings.SplitSeq(s, ".") {
		ok = ok && hostLabel(label)
	}
	if !ok {
		return fmt.Errorf("%w: smtp host must be a plain host name of 1-%d characters: dot-separated labels of 1-%d letters, digits and inner hyphens",
			ErrInvalidSettings, maxHostLen, maxLabelLen)
	}
	return nil
}

func hostLabel(l string) bool {
	return l != "" && len(l) <= maxLabelLen && !strings.HasPrefix(l, "-") && !strings.HasSuffix(l, "-") &&
		!strings.ContainsFunc(l, notLabelChar)
}

func notLabelChar(r rune) bool {
	isLabelChar := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
	return !isLabelChar
}

// checkLogin accepts 1 to maxLoginLen characters without control characters.
func checkLogin(s string) error {
	if s == "" || len(s) > maxLoginLen || strings.ContainsFunc(s, unicode.IsControl) {
		return fmt.Errorf("%w: smtp login must be 1-%d characters without control characters", ErrInvalidSettings, maxLoginLen)
	}
	return nil
}

func checkPort(p int) error {
	if p < 0 || p > maxPort {
		return fmt.Errorf("%w: smtp port must be 0 (the default of the mode) or 1-%d", ErrInvalidSettings, maxPort)
	}
	return nil
}

// checkHubURL accepts https://host[:port][/path], or plain http only to a loopback host (a local Hub). No user info,
// query or fragment: the API key goes only to this host and never in the URL.
func checkHubURL(s string) error {
	bad := func(why string) error { return fmt.Errorf("%w: hub url %s", ErrInvalidSettings, why) }
	if len(s) > maxHubURLLen {
		return bad("is too long")
	}
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return bad("does not parse")
	case u.Host == "" || u.Opaque != "":
		return bad("has no host")
	case u.User != nil:
		return bad("must not carry credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return bad("must not have a query or fragment")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return bad("must use https")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// checkMail validates the stored mail and Hub fields of a state; empty values are "not set".
func checkMail(s State) error {
	checks := []struct {
		set bool
		err func() error
	}{
		{s.HomeworkRecipient != "", func() error { return checkAddress(s.HomeworkRecipient) }},
		{s.SMTPHost != "", func() error { return checkHost(s.SMTPHost) }},
		{true, func() error { return checkPort(s.SMTPPort) }},
		{s.SMTPLogin != "", func() error { return checkLogin(s.SMTPLogin) }},
		{s.HubURL != "", func() error { return checkHubURL(s.HubURL) }},
	}
	for _, c := range checks {
		if c.set {
			if err := c.err(); err != nil {
				return err
			}
		}
	}
	if s.SMTPSecurity != "" && !s.SMTPSecurity.Valid() {
		return fmt.Errorf("%w: unknown smtp security %q", ErrInvalidSettings, s.SMTPSecurity)
	}
	return nil
}
