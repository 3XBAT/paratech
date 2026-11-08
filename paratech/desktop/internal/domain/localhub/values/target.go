package values

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Hosts of «Адрес сервера» that mean the Hub on this computer. [::1] is not one of them: the Hub listens on
// 127.0.0.1 only, so an IPv6 address would never reach it.
const (
	hostIPv4Loopback = "127.0.0.1"
	hostLocalhost    = "localhost"
)

// Target is the loopback port of a local Hub (ADR-0036 section 2). The zero value is not a target.
type Target struct{ port int }

// ParseTarget reads «Адрес сервера». It accepts only http with the host 127.0.0.1 or localhost, an explicit port
// 1..65535 written without leading zeros, and an empty path or a single "/"; no user info, query or fragment. Anything
// else, including an empty address, returns ErrNotLocal: the desktop then uses a remote Hub.
func ParseTarget(raw string) (Target, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return Target{}, fmt.Errorf("%w: not a URL", ErrNotLocal)
	case u.Scheme != "http" || u.Opaque != "":
		return Target{}, fmt.Errorf("%w: only http", ErrNotLocal)
	case u.User != nil:
		return Target{}, fmt.Errorf("%w: user info", ErrNotLocal)
	case u.RawQuery != "" || u.ForceQuery:
		return Target{}, fmt.Errorf("%w: a query", ErrNotLocal)
	case u.Fragment != "" || strings.Contains(raw, "#"):
		return Target{}, fmt.Errorf("%w: a fragment", ErrNotLocal)
	case u.RawPath != "" || (u.Path != "" && u.Path != "/"):
		return Target{}, fmt.Errorf("%w: a path", ErrNotLocal)
	}

	host := u.Hostname()
	if host != hostIPv4Loopback && !strings.EqualFold(host, hostLocalhost) {
		return Target{}, fmt.Errorf("%w: not a loopback host", ErrNotLocal)
	}
	p := u.Port()
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != p {
		return Target{}, fmt.Errorf("%w: no explicit port 1..65535", ErrNotLocal)
	}
	return Target{port: port}, nil
}

// Port returns the TCP port; 0 for the zero value.
func (t Target) Port() int { return t.port }

// IsZero reports whether t is not a target.
func (t Target) IsZero() bool { return t.port == 0 }

// ListenAddr is the address the local Hub listens on: always loopback, never every interface (ADR-0036 section 3).
func (t Target) ListenAddr() string { return hostIPv4Loopback + ":" + strconv.Itoa(t.port) }

// BaseURL is where the probe reaches the local Hub.
func (t Target) BaseURL() string { return "http://" + t.ListenAddr() }
