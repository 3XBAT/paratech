package values

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// AuthMode is the header the Hub sends the provider key in (HUB_ANTHROPIC_AUTH). The empty mode is the Hub default.
type AuthMode string

// Auth modes; the values are the Hub's.
const (
	AuthDefault AuthMode = ""
	AuthXAPIKey AuthMode = "x-api-key"
	AuthBearer  AuthMode = "bearer"
)

// Bounds of the base URL; maxBasePath mirrors the Hub's own check, so a saved URL never fails the Hub's config.
const (
	maxBaseURLLen = 2048
	maxBasePath   = 256
)

const unreservedChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"

// ServerSettings are the non-secret settings of the local Hub (ADR-0036 section 3). Zero values mean the Hub default:
// an empty base URL, AuthDefault and a quota limit of 0.
type ServerSettings struct {
	baseURL    string
	auth       AuthMode
	quotaLimit int64
}

// NewServerSettings validates the settings as the Hub does: the base URL is https, or http to a loopback host
// (localhost, 127.0.0.0/8, ::1), without user info, query or fragment, with an optional plain path such as /anthropic;
// the auth mode is x-api-key or bearer; the quota limit is 0 or above. It returns ErrInvalidSettings otherwise.
func NewServerSettings(baseURL string, auth AuthMode, quotaLimit int64) (ServerSettings, error) {
	if baseURL != "" {
		if err := checkBaseURL(baseURL); err != nil {
			return ServerSettings{}, err
		}
	}
	switch auth {
	case AuthDefault, AuthXAPIKey, AuthBearer:
	default:
		// Not quoted: a key pasted into the wrong field must not reach a log.
		return ServerSettings{}, fmt.Errorf("%w: auth mode must be %s or %s", ErrInvalidSettings, AuthXAPIKey, AuthBearer)
	}
	if quotaLimit < 0 {
		return ServerSettings{}, fmt.Errorf("%w: quota limit below 0", ErrInvalidSettings)
	}
	return ServerSettings{baseURL: baseURL, auth: auth, quotaLimit: quotaLimit}, nil
}

// BaseURL returns the provider base URL; empty for the Hub default.
func (s ServerSettings) BaseURL() string { return s.baseURL }

// Auth returns the auth mode; AuthDefault for the Hub default.
func (s ServerSettings) Auth() AuthMode { return s.auth }

// QuotaLimit returns the quota of every tester; 0 for the Hub default.
func (s ServerSettings) QuotaLimit() int64 { return s.quotaLimit }

func checkBaseURL(raw string) error {
	if len(raw) > maxBaseURLLen {
		return fmt.Errorf("%w: base URL longer than %d bytes", ErrInvalidSettings, maxBaseURLLen)
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c <= ' ' || c >= 0x7f || c == '\\' {
			return fmt.Errorf("%w: base URL contains a space, control, backslash or non-ASCII byte", ErrInvalidSettings)
		}
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || u.Opaque != "":
		return fmt.Errorf("%w: base URL must be absolute", ErrInvalidSettings)
	case u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname())):
		return fmt.Errorf("%w: base URL must be https (http only to a loopback host)", ErrInvalidSettings)
	case u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return fmt.Errorf("%w: base URL must not contain user info, a query or a fragment", ErrInvalidSettings)
	case !plainPath(u):
		return fmt.Errorf("%w: base URL path must be plain segments of at most %d characters", ErrInvalidSettings, maxBasePath)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, hostLocalhost) {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// plainPath accepts an empty path or segments of unreserved characters without dot or empty segments.
func plainPath(u *url.URL) bool {
	if u.RawPath != "" {
		return false
	}
	p := strings.TrimSuffix(u.Path, "/")
	if p == "" {
		return true
	}
	if len(p) > maxBasePath || !strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if seg == "" || seg == "." || seg == ".." || strings.TrimLeft(seg, unreservedChars) != "" {
			return false
		}
	}
	return true
}
