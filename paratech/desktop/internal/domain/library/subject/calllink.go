package subject

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Limits of a call link (ADR-0032).
const (
	MaxCallLinkBytes  = 2048
	maxCallHostBytes  = 253
	maxCallLabelBytes = 63
)

// CallLink is the link to the online class of a subject the student typed (ADR-0032, TASK-364), in canonical form:
// scheme and host in lowercase, path and query as given. The zero value is no link. It prints only its host
// (fmt, slog): the path or query may hold a meeting password.
type CallLink struct {
	link string
	host string
}

// ParseCallLink parses untrusted text by the rules of schedule/values.ParseCallLink (a copy, ARCH-11; the copies are
// cross-checked in desktop/tests): trimmed, at most MaxCallLinkBytes, printable ASCII without spaces, quotes,
// <>\^{|} or backtick, http or https, a host of lowercase a-z 0-9 . - with a dot and valid labels, no user info, port,
// IP literal, IDN or fragment, a path and query of RFC 3986 characters and %XX. Errors wrap ErrInvalidCallLink, name
// the rule and never quote the text. One addition over the schedule rule: text typed without a scheme gets "https://"
// in front first (see hasCallScheme), so the stored canonical form always passes the schedule rule.
func ParseCallLink(s string) (CallLink, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return CallLink{}, callLinkError("empty")
	case len(s) > MaxCallLinkBytes:
		return CallLink{}, callLinkError(fmt.Sprintf("longer than %d bytes", MaxCallLinkBytes))
	}
	if !hasCallScheme(s) {
		s = "https://" + s
		if len(s) > MaxCallLinkBytes {
			return CallLink{}, callLinkError(fmt.Sprintf("longer than %d bytes", MaxCallLinkBytes))
		}
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x21 || c > 0x7e {
			return CallLink{}, callLinkError("not printable ASCII without spaces")
		} else if strings.IndexByte("\"'<>\\^{|}`", c) >= 0 {
			return CallLink{}, callLinkError("forbidden character")
		}
	}
	if strings.IndexByte(s, '#') >= 0 {
		return CallLink{}, callLinkError("fragment not allowed")
	}

	scheme, rest, ok := strings.Cut(s, "://")
	if !ok {
		return CallLink{}, callLinkError("no http or https scheme")
	}
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return CallLink{}, callLinkError("scheme is not http or https")
	}

	end := strings.IndexAny(rest, "/?")
	if end < 0 {
		end = len(rest)
	}
	host, tail := strings.ToLower(rest[:end]), rest[end:]
	if err := checkCallHost(host); err != nil {
		return CallLink{}, err
	}
	if err := checkCallTail(tail); err != nil {
		return CallLink{}, err
	}
	return CallLink{link: scheme + "://" + host + tail, host: host}, nil
}

// String returns the canonical form, the one stored and opened; "" for no link.
func (l CallLink) String() string { return l.link }

// Host returns the host in lowercase, shown in full next to the link; "" for no link.
func (l CallLink) Host() string { return l.host }

// IsZero reports whether there is no link.
func (l CallLink) IsZero() bool { return l.link == "" }

// Format prints the host only, for every verb: %v of the struct would print the whole link.
func (l CallLink) Format(f fmt.State, _ rune) {
	if l.IsZero() {
		_, _ = io.WriteString(f, "no call link")
		return
	}
	_, _ = io.WriteString(f, "call link to "+l.host)
}

// LogValue keeps the path and query out of slog.
func (l CallLink) LogValue() slog.Value {
	if l.IsZero() {
		return slog.StringValue("no call link")
	}
	return slog.StringValue("call link to " + l.host)
}

// hasCallScheme reports whether s starts with "name:" before any '/' or '?', name a URI scheme (RFC 3986: a letter,
// then letters, digits, + - .), and the text after the colon up to '/' or '?' is not a port: "host:8080/x" has no
// scheme, "mailto:a@b", "javascript:alert(1)" and "ftp://x" have one. Text without a scheme gets "https://", so a
// misread is safe: the result is still checked as an https link and a port or user info in it is refused.
func hasCallScheme(s string) bool {
	if end := strings.IndexAny(s, "/?"); end >= 0 {
		s = s[:end]
	}
	name, after, ok := strings.Cut(s, ":")
	if !ok || name == "" || !isLetter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if c := name[i]; !isLetter(c) && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return !isDecimal(after)
}

func isLetter(c byte) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

func checkCallHost(host string) error {
	switch {
	case host == "":
		return callLinkError("no host")
	case strings.IndexByte(host, '@') >= 0:
		return callLinkError("user info not allowed")
	case strings.IndexByte(host, '[') >= 0 || strings.IndexByte(host, ']') >= 0:
		return callLinkError("IP address host not allowed")
	case strings.IndexByte(host, ':') >= 0:
		return callLinkError("port not allowed")
	case len(host) > maxCallHostBytes:
		return callLinkError("host too long")
	}
	for i := 0; i < len(host); i++ {
		if c := host[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return callLinkError("host has a character outside a-z 0-9 . -")
		}
	}

	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return callLinkError("host has no dot")
	}
	for _, label := range labels {
		switch {
		case label == "":
			return callLinkError("host has an empty label")
		case len(label) > maxCallLabelBytes:
			return callLinkError("host label too long")
		case label[0] == '-' || label[len(label)-1] == '-':
			return callLinkError("host label starts or ends with a hyphen")
		case strings.HasPrefix(label, "xn--"):
			return callLinkError("international host not allowed")
		}
	}
	// A browser reads a host whose last label is a number (decimal or 0x hex) as an IPv4 address.
	if last := labels[len(labels)-1]; isDecimal(last) || isHexNumber(last) {
		return callLinkError("IP address host not allowed")
	}
	return nil
}

// checkCallTail checks the path and query: "" or starting with '/' or '?', RFC 3986 characters only, every '%'
// followed by two hex digits.
func checkCallTail(tail string) error {
	for i := 0; i < len(tail); i++ {
		c := tail[i]
		switch {
		case c == '%':
			if i+2 >= len(tail) || !isHex(tail[i+1]) || !isHex(tail[i+2]) {
				return callLinkError("bad percent escape")
			}
			i += 2
		case isUnreserved(c) || strings.IndexByte("!$&()*+,;=:@/?", c) >= 0:
		default:
			return callLinkError("character not allowed in path or query")
		}
	}
	return nil
}

func isUnreserved(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func isDecimal(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func isHexNumber(s string) bool {
	digits, ok := strings.CutPrefix(s, "0x")
	if !ok {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if !isHex(digits[i]) {
			return false
		}
	}
	return true
}

func callLinkError(rule string) error { return fmt.Errorf("%w: %s", ErrInvalidCallLink, rule) }
