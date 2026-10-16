package googlecal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// Defaults and bounds of the client.
const (
	// DefaultBaseURL is the Calendar API v3; production never uses another base.
	DefaultBaseURL = "https://www.googleapis.com/calendar/v3"
	// DefaultRequestTimeout bounds one HTTP request (a call makes at most a few).
	DefaultRequestTimeout = 30 * time.Second

	// CalendarIDName is the entry of the app calendar in the google credential store ("paratech/google/calendar-id").
	CalendarIDName = "calendar-id"

	// maxResponseBytes bounds one answer; a calendar or an event is a few kilobytes.
	maxResponseBytes = 4 << 20
	// maxRequestBytes bounds one request body; events are a few hundred bytes.
	maxRequestBytes = 64 << 10
)

// Operation names, for error texts.
const (
	opNew            = "googlecal: new"
	opEnsure         = "googlecal: ensure calendar"
	opConfirm        = "googlecal: confirm calendar"
	opForget         = "googlecal: forget calendar"
	opUpsertLesson   = "googlecal: upsert lesson"
	opDeleteEvent    = "googlecal: delete event"
	opUpsertDeadline = "googlecal: upsert deadline"
)

// Auth is the Google authorization (googleauth.Authorizer in the app).
type Auth interface {
	// HTTPClient returns a client that adds "Authorization: Bearer <access token>" to every request, refreshing the
	// token as needed.
	HTTPClient() *http.Client
	// ForgetAccessToken drops the cached access token, so the next request refreshes it.
	ForgetAccessToken()
}

// Store is the google credential store (wincred.Store in the app, testenv.MemCredentials in tests).
type Store interface {
	// Get returns the value under name, or an error wrapping shared.ErrNotFound.
	Get(ctx context.Context, name string) (string, error)
	// Set stores or replaces the value under name.
	Set(ctx context.Context, name, value string) error
	// Delete removes the value under name; a missing value is not an error.
	Delete(ctx context.Context, name string) error
}

// Option changes a default of New.
type Option func(*Client)

// WithBaseURL replaces DefaultBaseURL (tests: a fake Calendar API). It must be https, or http to a loopback host.
func WithBaseURL(base string) Option { return func(c *Client) { c.base = base } }

// WithRequestTimeout replaces DefaultRequestTimeout.
func WithRequestTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// Client is the Google Calendar adapter. It is safe for concurrent use.
type Client struct {
	auth     Auth
	store    Store
	classify AuthClassifier
	base     string
	timeout  time.Duration

	// mu serializes the calendar binding: two parallel EnsureCalendar calls must not create two calendars.
	mu sync.Mutex
}

// New returns a Client over the authorization, the google credential store and the classifier of token source errors.
func New(auth Auth, store Store, classify AuthClassifier, opts ...Option) (*Client, error) {
	if auth == nil || store == nil || classify == nil {
		return nil, fmt.Errorf("%s: %w: auth, store and classifier are required", opNew, ErrInvalidConfig)
	}
	c := &Client{auth: auth, store: store, classify: classify, base: DefaultBaseURL, timeout: DefaultRequestTimeout}
	for _, opt := range opts {
		opt(c)
	}
	if c.timeout <= 0 {
		return nil, fmt.Errorf("%s: %w: the request timeout must be positive", opNew, ErrInvalidConfig)
	}
	base, err := checkBaseURL(c.base)
	if err != nil {
		return nil, err
	}
	c.base = base
	return c, nil
}

// checkBaseURL accepts https://host[:port]/path, or http only for a loopback host; it drops a trailing slash.
func checkBaseURL(raw string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%s: %w: base url %s", opNew, ErrInvalidConfig, why)
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return bad("does not parse")
	case u.Host == "" || u.Opaque != "":
		return bad("has no host")
	case u.User != nil:
		return bad("must not carry credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return bad("must not have a query or fragment")
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return bad("must use https")
	}
	return strings.TrimRight(raw, "/"), nil
}

func isLoopback(host string) bool {
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// request is one Calendar API call.
type request struct {
	method string
	path   string // escaped, below the base URL
	query  url.Values
	body   any   // JSON body, or nil
	out    any   // decoded on 2xx, or nil
	accept []int // non-2xx statuses returned without an error
}

// googleError is the error body of the Calendar API; only the reasons are used.
type googleError struct {
	Error struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

// do runs r and returns the status: a 2xx or one of r.accept. A 401 drops the access token and is retried once.
func (c *Client) do(ctx context.Context, op string, r request) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	var body []byte
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil || len(b) > maxRequestBytes {
			return 0, permanent(op, ErrInvalidRequest)
		}
		body = b
	}
	target := c.base + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	for attempt := 1; ; attempt++ {
		status, err := c.once(ctx, op, r, target, body)
		if status == http.StatusUnauthorized {
			c.auth.ForgetAccessToken()
			if attempt == 1 {
				continue
			}
			return 0, statusError(op, status, nil, 0)
		}
		return status, err
	}
}

// once sends one request.
func (c *Client) once(ctx context.Context, op string, r request, target string, body []byte) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, r.method, target, reader)
	if err != nil {
		return 0, permanent(op, ErrInvalidRequest)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client().Do(req)
	if err != nil {
		return 0, c.transportError(ctx, reqCtx, op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	switch {
	case err != nil:
		return 0, c.transportError(ctx, reqCtx, op, err)
	case len(data) > maxResponseBytes:
		return 0, transient(op, ErrBadResponse)
	}

	status := resp.StatusCode
	if status >= 200 && status < 300 {
		if r.out != nil && json.Unmarshal(data, r.out) != nil {
			return 0, transient(op, ErrBadResponse)
		}
		return status, nil
	}
	if status == http.StatusUnauthorized {
		return status, nil // do retries it
	}
	var ge googleError
	_ = json.Unmarshal(data, &ge) // the reasons are optional; never put the body in an error
	reasons := make([]string, 0, len(ge.Error.Errors))
	for _, e := range ge.Error.Errors {
		reasons = append(reasons, e.Reason)
	}
	// A rate limit is never an accepted answer: a 403 "slow down" must not read as "no access".
	rateLimited := status == http.StatusForbidden && hasRateLimitReason(reasons)
	if !rateLimited && slices.Contains(r.accept, status) {
		return status, nil
	}
	return 0, statusError(op, status, reasons, parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()))
}

// client is the bearer client without redirects: the Calendar API does not redirect, and the token must not follow one.
func (c *Client) client() *http.Client {
	hc := *c.auth.HTTPClient()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

// transportError classifies a request without an answer: a cancelled call, a timeout, a token source failure or the
// network.
func (c *Client) transportError(ctx, reqCtx context.Context, op string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}
	if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
		return transient(op, ErrTimeout)
	}
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err // *url.Error itself is a net.Error, so look inside
	}
	if e := authError(op, c.classify(inner)); e != nil {
		return e
	}
	var ne net.Error
	if errors.As(inner, &ne) || errors.Is(inner, shared.ErrNetworkUnavailable) || errors.Is(inner, io.EOF) ||
		errors.Is(inner, io.ErrUnexpectedEOF) {
		return transient(op, ErrNetwork)
	}
	return transient(op, ErrUnavailable)
}

// calendarPath is the escaped path of a calendar, plus more escaped segments.
func calendarPath(calendarID string, more ...string) string {
	p := "/calendars/" + url.PathEscape(calendarID)
	for _, s := range more {
		p += "/" + url.PathEscape(s)
	}
	return p
}
