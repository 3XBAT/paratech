package values

import (
	"fmt"
	"net/url"
	"strings"
)

// MaxSourceURLLen bounds a source URL.
const MaxSourceURLLen = 2048

// schemeHTTPS is the only scheme a model source may use.
const schemeHTTPS = "https"

// SourceURL is where a catalog model can be downloaded from (PRD section 8.1). It is data only: the domain never
// fetches it, the download adapter (TASK-027) does, and only for hosts the catalog lists.
//
// Accepted: absolute https URLs with an ASCII host, a non-root path and no user info, query or fragment. Query
// strings are refused because they are where signed or tokenised download links carry credentials.
type SourceURL struct{ raw string }

// ParseSourceURL validates s and returns the URL in canonical form (lowercase scheme and host).
func ParseSourceURL(s string) (SourceURL, error) {
	if s == "" || len(s) > MaxSourceURLLen {
		return SourceURL{}, fmt.Errorf("%w: empty or longer than %d bytes", ErrInvalidSourceURL, MaxSourceURLLen)
	}
	for i := 0; i < len(s); i++ {
		// Only printable ASCII; a backslash is a path separator for some URL parsers.
		if c := s[i]; c <= ' ' || c >= 0x7f || c == '\\' {
			return SourceURL{}, fmt.Errorf("%w: contains a space, control, backslash or non-ASCII byte", ErrInvalidSourceURL)
		}
	}

	u, err := url.Parse(s)
	if err != nil {
		return SourceURL{}, fmt.Errorf("%w: %w", ErrInvalidSourceURL, err)
	}
	switch {
	case u.Scheme != schemeHTTPS:
		return SourceURL{}, fmt.Errorf("%w: scheme %q, only https is allowed", ErrInvalidSourceURL, u.Scheme)
	case u.Opaque != "":
		return SourceURL{}, fmt.Errorf("%w: not a hierarchical URL", ErrInvalidSourceURL)
	case u.User != nil:
		return SourceURL{}, fmt.Errorf("%w: user info is not allowed", ErrInvalidSourceURL)
	case u.Hostname() == "":
		return SourceURL{}, fmt.Errorf("%w: no host", ErrInvalidSourceURL)
	case u.RawQuery != "" || u.ForceQuery:
		return SourceURL{}, fmt.Errorf("%w: a query is not allowed", ErrInvalidSourceURL)
	case u.Fragment != "" || strings.Contains(s, "#"):
		return SourceURL{}, fmt.Errorf("%w: a fragment is not allowed", ErrInvalidSourceURL)
	case u.Path == "" || u.Path == "/":
		return SourceURL{}, fmt.Errorf("%w: no file path", ErrInvalidSourceURL)
	}

	u.Host = strings.ToLower(u.Host)
	return SourceURL{raw: u.String()}, nil
}

// String returns the canonical URL; empty for the zero value.
func (u SourceURL) String() string { return u.raw }

// IsZero reports whether no URL is set.
func (u SourceURL) IsZero() bool { return u.raw == "" }

// Host returns the lowercase host (with the port, if any), for an allowlist of download hosts.
func (u SourceURL) Host() string {
	parsed, err := url.Parse(u.raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}
