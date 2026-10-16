// Package weblink opens a web page in the user's default browser, for the source page of a model (PRD section 8.1:
// open source page). On Windows it starts explorer.exe from the Windows directory with the URL as its only argument:
// no shell, no cmd.exe, no rundll32.
//
// The URL is re-serialized from the parsed values.SourceURL and checked right before the start: https, a plain host,
// and a path of unreserved ASCII characters only. Anything that explorer.exe or Windows could read as a switch, a
// separator, a quote or an environment variable is refused. OpenAuthPage is the same launcher for an OAuth
// authorization page, whose query is checked byte by byte instead (unreserved characters, '=', '&', "%XX").
// explorer.exe opens a folder instead of the browser for any URL with a query, so an authorization page is written to
// a temporary Internet Shortcut (.url) file and explorer.exe opens that file. explorer.exe stays the launcher in both
// cases so the browser is not started in our process tree (ADR-0002: whole-computer capture excludes our tree).
package weblink

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"paratech/desktop/internal/domain/transcription/values"
)

var (
	// ErrUnsupported means this system has no browser launcher (only Windows has one).
	ErrUnsupported = errors.New("weblink: opening a web page is supported only on Windows")
	// ErrRefused means the URL failed the check before the start; nothing was started.
	ErrRefused = errors.New("weblink: the link is not a plain https page")
)

// maxLinkLen bounds the argument; far below the command-line limit of Windows. maxAuthLinkLen bounds an
// authorization page with its query.
const (
	maxLinkLen     = 1024
	maxAuthLinkLen = 2048
)

const httpsPrefix = "https://"

// Opener opens pages in the default browser. The zero value is ready to use.
type Opener struct{}

// New returns an Opener.
func New() *Opener { return &Opener{} }

// OpenLink opens u in the default browser. It returns once the launcher has started (Windows) or fails with
// ErrRefused, ErrUnsupported or a start error.
func (o *Opener) OpenLink(ctx context.Context, u values.SourceURL) error {
	arg, err := linkArg(u)
	if err != nil {
		return err
	}
	return start(ctx, arg)
}

// OpenAuthPage opens an OAuth authorization page (TASK-050: Google sign-in in the system browser). The URL is checked
// as is, never rebuilt: https, a plain host, a path as for OpenLink and a non-empty query of unreserved characters,
// '=', '&' and percent escapes of two hex digits only. It returns like OpenLink.
func (o *Opener) OpenAuthPage(ctx context.Context, rawURL string) error {
	if err := checkAuthLink(rawURL); err != nil {
		return err
	}
	return startShortcut(ctx, rawURL)
}

// checkAuthLink accepts "https://<host><path>?<query>" where the part before '?' passes checkLink and the query is
// unreserved characters, '=', '&' and "%XX" escapes. It refuses '+', ',', ';', quotes, spaces, '^', '!', '<', '>',
// '|', '`', a bare '%', a fragment and everything else.
func checkAuthLink(s string) error {
	if len(s) > maxAuthLinkLen {
		return fmt.Errorf("%w: longer than %d bytes", ErrRefused, maxAuthLinkLen)
	}
	base, query, ok := strings.Cut(s, "?")
	if !ok || query == "" {
		return fmt.Errorf("%w: an authorization page needs a query", ErrRefused)
	}
	if err := checkLink(base); err != nil {
		return err
	}
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '%':
			if i+2 >= len(query) || !isHex(query[i+1]) || !isHex(query[i+2]) {
				return fmt.Errorf("%w: the query has a bad escape", ErrRefused)
			}
			i += 2
		case c == '=' || c == '&' || (pathByte(c) && c != '/'):
		default:
			return fmt.Errorf("%w: the query has a character that is not allowed", ErrRefused)
		}
	}
	if _, err := url.Parse(s); err != nil {
		return fmt.Errorf("%w: not a URL", ErrRefused)
	}
	return nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// linkArg re-serializes u and returns it when it passes the check.
func linkArg(u values.SourceURL) (string, error) {
	parsed, err := url.Parse(u.String())
	if err != nil {
		return "", fmt.Errorf("%w: not a URL", ErrRefused)
	}
	if parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Port() != "" {
		return "", fmt.Errorf("%w: only https with a host and a path", ErrRefused)
	}
	arg := httpsPrefix + parsed.Host + parsed.Path
	if err := checkLink(arg); err != nil {
		return "", err
	}
	return arg, nil
}

// checkLink accepts "https://<host><path>" where the host is lowercase letters, digits, '-' and '.', and the path
// is unreserved characters (RFC 3986) and '/'. It refuses spaces, quotes, ',', ';', '%', '^', '&', '=', '!' and
// every other character the Windows command line, explorer.exe or ShellExecute treat specially.
func checkLink(s string) error {
	if len(s) > maxLinkLen || !strings.HasPrefix(s, httpsPrefix) {
		return fmt.Errorf("%w: not an https URL of at most %d bytes", ErrRefused, maxLinkLen)
	}
	rest := s[len(httpsPrefix):]
	host, path, ok := strings.Cut(rest, "/")
	if !ok || !validHost(host) {
		return fmt.Errorf("%w: invalid host", ErrRefused)
	}
	path = "/" + path
	if path == "/" || strings.Contains(path, "//") {
		return fmt.Errorf("%w: no page path", ErrRefused)
	}
	for i := 0; i < len(path); i++ {
		if !pathByte(path[i]) {
			return fmt.Errorf("%w: the path has a character that is not allowed", ErrRefused)
		}
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: dot segment in the path", ErrRefused)
		}
	}
	return nil
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 || h[0] == '-' || h[0] == '.' || h[len(h)-1] == '-' || h[len(h)-1] == '.' ||
		!strings.Contains(h, ".") || strings.Contains(h, "..") {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func pathByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return c == '-' || c == '.' || c == '_' || c == '~' || c == '/'
}
