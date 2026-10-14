// Package httpdownload fetches model files over HTTPS with ranged requests (PRD sections 8 and 8.1). It satisfies the
// ModelSource port of modelcatalog structurally and only transfers bytes: what a reply means for the partial file is
// decided by the use case with the catalog entry.
//
// Outbound policy (PRD section 13.2, no arbitrary outbound URLs): the request and every redirect must pass the
// catalog allowlist (https, allowlisted host); the default transport dials only public unicast addresses, uses no
// proxy, no cookies and no compression, and a transfer that sends no byte for the idle timeout is stopped.
package httpdownload

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"paratech/desktop/internal/domain/transcription/values"
)

// Defaults of the client.
const (
	DefaultIdleTimeout = 60 * time.Second

	maxRedirects     = 5
	userAgent        = "paratech-desktop"
	dialTimeout      = 30 * time.Second
	tlsTimeout       = 15 * time.Second
	headerTimeout    = 30 * time.Second
	idleConnTimeout  = 90 * time.Second
	maxDrainBytes    = 64 << 10
	byteRangesPrefix = "bytes "
)

// Operation names used in error messages.
const (
	opFetch    = "httpdownload: fetch"
	opRedirect = "httpdownload: redirect"
	opRead     = "httpdownload: read"
)

// Typed errors of the adapter (ARCH-32).
var (
	// ErrTransfer is a network failure of a request or of the body (connection refused or reset, TLS, ...).
	ErrTransfer = errors.New("httpdownload: transfer failed")

	// ErrStalled means the server sent nothing for the idle timeout; it wraps ErrTransfer.
	ErrStalled = fmt.Errorf("%w: no data within the idle timeout", ErrTransfer)

	// ErrForbiddenAddress means a host resolved to an address that is not public (loopback, private, link-local,
	// ...); it is reported wrapped in ErrTransfer.
	ErrForbiddenAddress = errors.New("httpdownload: address is not public")

	// ErrStatus is an HTTP status other than 200, 206 and 416; the error is a *StatusError.
	ErrStatus = errors.New("httpdownload: unexpected http status")

	// ErrBadReply is a reply whose headers do not describe one plain byte range of the file.
	ErrBadReply = errors.New("httpdownload: malformed reply")

	// ErrTooManyRedirects means the source redirected more than maxRedirects times.
	ErrTooManyRedirects = errors.New("httpdownload: too many redirects")
)

// StatusError carries the HTTP status of a refused request.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("%v: %d", ErrStatus, e.Code) }

// Unwrap returns ErrStatus.
func (e *StatusError) Unwrap() error { return ErrStatus }

// Client fetches model files. Create it with New; it is safe for concurrent use.
type Client struct {
	hc   *http.Client
	idle time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithIdleTimeout sets how long a request may wait for headers or the next byte; non-positive values are ignored.
func WithIdleTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.idle = d
		}
	}
}

// WithHTTPClient replaces the transport, for tests against an httptest TLS server. The redirect policy (the
// allowlist), https only, no cookies and the idle timeout still apply; the public-address rule is part of the
// default transport only.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// New returns a Client with the hardened default transport.
func New(opts ...Option) *Client {
	c := &Client{hc: defaultHTTPClient(), idle: DefaultIdleTimeout}
	for _, o := range opts {
		o(c)
	}
	return c
}

func defaultHTTPClient() *http.Client {
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second, Control: publicAddressOnly}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 nil, // a proxy would hide the address the dialer checks
		DialContext:           d.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: headerTimeout,
		DisableCompression:    true, // byte ranges are offsets into the stored file
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		IdleConnTimeout:       idleConnTimeout,
	}}
}

// Fetch requests src from byte offset; 0 asks for the whole file without a Range header. The reply is 200
// (RangeFull, Total from Content-Length), 206 (RangePartial, Start and Total from Content-Range) or 416
// (RangeNotSatisfiable, empty body). The caller closes the body.
func (c *Client) Fetch(ctx context.Context, src values.SourceURL, offset int64, allow values.HostAllowlist) (values.RangeReply, io.ReadCloser, error) {
	if offset < 0 {
		return values.RangeReply{}, nil, fmt.Errorf("%s: negative offset %d", opFetch, offset)
	}
	if err := allow.Check(src.String()); err != nil {
		return values.RangeReply{}, nil, fmt.Errorf("%s: %w", opFetch, err)
	}

	rctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, src.String(), nil)
	if err != nil {
		cancel()
		return values.RangeReply{}, nil, fmt.Errorf("%s: %w: %w", opFetch, values.ErrInvalidSourceURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}

	hc := *c.hc
	hc.Jar = nil
	hc.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return ErrTooManyRedirects
		}
		if err := allow.Check(r.URL.String()); err != nil {
			return fmt.Errorf("%s: %w", opRedirect, err)
		}
		return nil
	}

	w := newWatchdog(c.idle, cancel)
	resp, err := hc.Do(req)
	if err != nil {
		w.stop()
		cancel()
		return values.RangeReply{}, nil, fmt.Errorf("%s: %w", opFetch, transferErr(ctx, w, err))
	}

	reply, err := parseReply(resp)
	if err == nil {
		err = allow.Check(resp.Request.URL.String()) // defense in depth: CheckRedirect saw every hop
	}
	if err != nil || reply.Status == values.RangeNotSatisfiable {
		_, _ = io.CopyN(io.Discard, resp.Body, maxDrainBytes)
		_ = resp.Body.Close()
		w.stop()
		cancel()
		if err != nil {
			return values.RangeReply{}, nil, fmt.Errorf("%s: %w", opFetch, err)
		}
		return reply, http.NoBody, nil
	}

	w.kick()
	return reply, &body{rc: resp.Body, parent: ctx, w: w, cancel: cancel}, nil
}

// transferErr maps an error of the transport: the caller's cancellation as it is, policy refusals as they are, the
// rest wrapped in ErrTransfer.
func transferErr(parent context.Context, w *watchdog, err error) error {
	switch {
	case parent.Err() != nil:
		return parent.Err()
	case w.fired.Load():
		return ErrStalled
	case errors.Is(err, values.ErrHostNotAllowed), errors.Is(err, values.ErrInvalidSourceURL),
		errors.Is(err, values.ErrInvalidHost), errors.Is(err, ErrTooManyRedirects):
		return err
	}
	return fmt.Errorf("%w: %w", ErrTransfer, err)
}

// parseReply turns the status and headers into a RangeReply.
func parseReply(resp *http.Response) (values.RangeReply, error) {
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return values.RangeReply{}, fmt.Errorf("%w: content encoding %q", ErrBadReply, enc)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		total := resp.ContentLength
		if total < 0 {
			total = values.UnknownTotal
		}
		return values.RangeReply{Status: values.RangeFull, Total: total}, nil

	case http.StatusPartialContent:
		if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && strings.HasPrefix(mt, "multipart/") {
			return values.RangeReply{}, fmt.Errorf("%w: multipart range reply", ErrBadReply)
		}
		start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
		if err != nil {
			return values.RangeReply{}, err
		}
		if start < 0 {
			return values.RangeReply{}, fmt.Errorf("%w: 206 without a byte range", ErrBadReply)
		}
		if resp.ContentLength >= 0 && resp.ContentLength != end-start+1 {
			return values.RangeReply{}, fmt.Errorf("%w: content length %d for range %d-%d", ErrBadReply, resp.ContentLength, start, end)
		}
		return values.RangeReply{Status: values.RangePartial, Start: start, Total: total}, nil

	case http.StatusRequestedRangeNotSatisfiable:
		total := values.UnknownTotal
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			start, _, t, err := parseContentRange(cr)
			if err != nil || start >= 0 {
				return values.RangeReply{}, fmt.Errorf("%w: 416 with content range %q", ErrBadReply, cr)
			}
			total = t
		}
		return values.RangeReply{Status: values.RangeNotSatisfiable, Total: total}, nil
	}
	return values.RangeReply{}, &StatusError{Code: resp.StatusCode}
}

// parseContentRange parses "bytes <start>-<end>/<total|*>" and "bytes */<total>" (RFC 9110 section 14.4). The
// unsatisfied form returns start -1; an unknown total is values.UnknownTotal.
func parseContentRange(s string) (start, end, total int64, err error) {
	bad := fmt.Errorf("%w: content range %q", ErrBadReply, s)
	rest, ok := strings.CutPrefix(s, byteRangesPrefix)
	if !ok {
		return 0, 0, 0, bad
	}
	rng, size, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, 0, 0, bad
	}

	total = values.UnknownTotal
	if size != "*" {
		if total, ok = parseDigits(size); !ok {
			return 0, 0, 0, bad
		}
	}

	if rng == "*" {
		if total == values.UnknownTotal {
			return 0, 0, 0, bad
		}
		return -1, -1, total, nil
	}
	first, last, ok := strings.Cut(rng, "-")
	if !ok {
		return 0, 0, 0, bad
	}
	if start, ok = parseDigits(first); !ok {
		return 0, 0, 0, bad
	}
	if end, ok = parseDigits(last); !ok || end < start || (total != values.UnknownTotal && end >= total) {
		return 0, 0, 0, bad
	}
	return start, end, total, nil
}

// parseDigits accepts only ASCII digits (no sign, no spaces) that fit an int64.
func parseDigits(s string) (int64, bool) {
	if s == "" || len(s) > 19 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// body is the response body with the idle watchdog; errors are mapped like the request's.
type body struct {
	rc     io.ReadCloser
	parent context.Context
	w      *watchdog
	cancel context.CancelFunc
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.w.kick()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("%s: %w", opRead, transferErr(b.parent, b.w, err))
	}
	return n, err
}

func (b *body) Close() error {
	b.w.stop()
	err := b.rc.Close()
	b.cancel()
	return err
}

// watchdog cancels a request that makes no progress for the idle timeout.
type watchdog struct {
	idle  time.Duration
	t     *time.Timer
	fired atomic.Bool
}

func newWatchdog(idle time.Duration, cancel context.CancelFunc) *watchdog {
	w := &watchdog{idle: idle}
	w.t = time.AfterFunc(idle, func() {
		w.fired.Store(true)
		cancel()
	})
	return w
}

func (w *watchdog) kick() { w.t.Reset(w.idle) }

func (w *watchdog) stop() { w.t.Stop() }

// nonPublic are ranges outside the netip classification that must not be dialed either.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach any IPv4 address
}

// publicAddressOnly is the dialer Control: it refuses every address that is not public unicast, after DNS, so a
// catalog host that resolves to a local address (DNS rebinding) cannot reach the machine or the LAN.
func publicAddressOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrForbiddenAddress, address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrForbiddenAddress, address)
	}
	ip = ip.Unmap()

	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" {
		return fmt.Errorf("%w: %s", ErrForbiddenAddress, ip)
	}
	for _, p := range nonPublic {
		if p.Contains(ip) {
			return fmt.Errorf("%w: %s", ErrForbiddenAddress, ip)
		}
	}
	return nil
}
