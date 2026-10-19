package hubclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
	"paratech/shared/gen/paratech/hub/v1/hubv1connect"
)

// Defaults and bounds of the client.
const (
	DefaultCredentialName = "api-key"
	DefaultRequestTimeout = 2 * time.Minute

	// maxResponseBytes bounds one answer: 1 MiB of output is far above any operation, plus envelope headroom.
	maxResponseBytes = 2 << 20

	// clientGrace lets the per-call context fire before the http.Client backstop timeout.
	clientGrace = 5 * time.Second

	dialTimeout   = 30 * time.Second
	tlsTimeout    = 15 * time.Second
	idleConnLimit = 90 * time.Second
)

// Operation names used in error messages.
const (
	opNew       = "hubclient: new"
	opComplete  = "hubclient: complete"
	opRemaining = "hubclient: get remaining"
)

// Credentials is the part of the secret store the client needs (adapters/wincred.Store, testenv.MemCredentials).
// Get returns an error wrapping shared.ErrNotFound when no secret is stored under name.
type Credentials interface {
	Get(ctx context.Context, name string) (string, error)
}

// URLSource gives the Hub base URL at call time (the local settings in the app), so a changed URL applies to the next
// call without a restart.
type URLSource interface {
	// HubURL returns the configured base URL; empty means the Hub is not configured.
	HubURL(ctx context.Context) (string, error)
}

// Config configures a Client. Zero values take the defaults.
type Config struct {
	// BaseURL of the Hub: https, or http to a loopback host (tests, local Hub). Exactly one of BaseURL and URLs is set.
	BaseURL string

	// URLs gives the base URL on every call, checked by the same rule as BaseURL.
	URLs URLSource

	// CredentialName is the name of the API key in the credential store; default DefaultCredentialName.
	CredentialName string

	// RequestTimeout bounds one call; default DefaultRequestTimeout.
	RequestTimeout time.Duration

	// HTTPClient replaces the default transport (tests). The client copies it and always refuses redirects.
	HTTPClient *http.Client
}

// Client calls the Hub gateway. It is safe for concurrent use.
type Client struct {
	urls     URLSource
	http     *http.Client
	creds    Credentials
	credName string
	timeout  time.Duration
}

// fixedURL is the URLSource of Config.BaseURL.
type fixedURL string

func (u fixedURL) HubURL(context.Context) (string, error) { return string(u), nil }

// New validates cfg and builds a client.
func New(cfg Config, creds Credentials) (*Client, error) {
	if creds == nil {
		return nil, fmt.Errorf("%s: %w: no credential store", opNew, ErrInvalidConfig)
	}
	urls := cfg.URLs
	switch {
	case urls != nil && cfg.BaseURL != "":
		return nil, fmt.Errorf("%s: %w: both a base url and a url source", opNew, ErrInvalidConfig)
	case urls == nil:
		base, err := checkBaseURL(opNew, cfg.BaseURL)
		if err != nil {
			return nil, err
		}
		urls = fixedURL(base)
	}
	if cfg.RequestTimeout < 0 {
		return nil, fmt.Errorf("%s: %w: negative request timeout", opNew, ErrInvalidConfig)
	}

	c := &Client{urls: urls, creds: creds, credName: cfg.CredentialName, timeout: cfg.RequestTimeout}
	if c.credName == "" {
		c.credName = DefaultCredentialName
	}
	if c.timeout == 0 {
		c.timeout = DefaultRequestTimeout
	}
	c.http = httpClientFor(cfg.HTTPClient, c.timeout)
	return c, nil
}

// Complete runs one operation on the Hub (homeworkflow.LLMClient). See the package doc for the error contract.
func (c *Client) Complete(ctx context.Context, req values.LLMRequest) (values.LLMResult, error) {
	if err := ctx.Err(); err != nil {
		return values.LLMResult{}, fmt.Errorf("%s: %w", opComplete, err)
	}
	spec, err := specOf(req)
	if err != nil {
		return values.LLMResult{}, err
	}
	base, err := c.baseURL(ctx, opComplete)
	if err != nil {
		return values.LLMResult{}, err
	}
	key, err := c.apiKey(ctx, opComplete)
	if err != nil {
		return values.LLMResult{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Building the generated client per call is cheap; the http.Client and its connections are shared.
	gateway := hubv1connect.NewGatewayServiceClient(c.http, base, connect.WithReadMaxBytes(maxResponseBytes))
	creq := connect.NewRequest(&hubv1.CompleteRequest{Spec: spec})
	creq.Header().Set("Authorization", "Bearer "+key)
	resp, err := gateway.Complete(callCtx, creq)
	if err != nil {
		if perr := ctx.Err(); perr != nil {
			return values.LLMResult{}, fmt.Errorf("%s: %w", opComplete, perr)
		}
		// Our own deadline: Connect may report it as unavailable or canceled, depending on where the call was.
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return values.LLMResult{}, transient(opComplete, ErrTimeout)
		}
		return values.LLMResult{}, mapCallError(opComplete, err)
	}
	return resultOf(resp.Msg)
}

// specOf copies exactly the contract fields of the request; nothing else reaches the wire.
func specOf(req values.LLMRequest) (*hubv1.CompletionSpec, error) {
	if req.IsZero() {
		return nil, permanent(opComplete, ErrInvalidRequest)
	}
	op, ok := operationType(req.Operation())
	if !ok {
		return nil, permanent(opComplete, ErrInvalidRequest)
	}
	return &hubv1.CompletionSpec{
		IdempotencyKey: req.IdempotencyKey(),
		OperationType:  op,
		ModelAlias:     req.ModelAlias(),
		Instructions:   req.Instructions(),
		UntrustedData:  req.UntrustedData(),
	}, nil
}

func operationType(op values.Operation) (hubv1.OperationType, bool) {
	switch op {
	case values.OperationSummary:
		return hubv1.OperationType_OPERATION_TYPE_SUMMARY, true
	case values.OperationExtractHomework:
		return hubv1.OperationType_OPERATION_TYPE_EXTRACT_HOMEWORK_DEADLINES, true
	case values.OperationHomeworkDraft:
		return hubv1.OperationType_OPERATION_TYPE_HOMEWORK_DRAFT, true
	}
	return hubv1.OperationType_OPERATION_TYPE_UNSPECIFIED, false
}

// baseURL reads the Hub URL for one call. No URL, or a stored URL the client refuses, parks the job (notConfigured)
// instead of failing it, so its key stays usable once the user fixes the setting. A refused Config.BaseURL never
// gets here: New refuses it.
func (c *Client) baseURL(ctx context.Context, op string) (string, error) {
	raw, err := c.urls.HubURL(ctx)
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "", fmt.Errorf("%s: %w", op, err)
	default:
		return "", transient(op, ErrSettings)
	}
	if raw == "" {
		return "", notConfigured(ErrNoURL)
	}
	base, err := checkBaseURL(op, raw)
	if err != nil {
		return "", notConfigured(ErrInvalidConfig)
	}
	return base, nil
}

// apiKey reads the key for one call. The store's errors carry no secret (wincred contract), so they are wrapped.
// A missing key parks the job like a missing URL.
func (c *Client) apiKey(ctx context.Context, op string) (string, error) {
	key, err := c.creds.Get(ctx, c.credName)
	switch {
	case err == nil:
	case errors.Is(err, shared.ErrNotFound):
		return "", notConfigured(ErrMissingAPIKey)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "", fmt.Errorf("%s: %w", op, err)
	case errors.Is(err, errors.ErrUnsupported), errors.Is(err, shared.ErrInvalidID):
		return "", permanent(op, fmt.Errorf("%w: %w", ErrCredentials, err))
	default:
		return "", transient(op, fmt.Errorf("%w: %w", ErrCredentials, err))
	}

	// The same rule the page applies when the key is entered (values.ParseHubAPIKey), so the header cannot be split.
	if !values.ValidHubAPIKey(key) {
		return "", permanent(op, ErrMalformedAPIKey)
	}
	return key, nil
}

// resultOf converts the answer; the output stays untrusted (hard rule 3).
func resultOf(resp *hubv1.CompleteResponse) (values.LLMResult, error) {
	res := resp.GetResult()
	if res == nil {
		return values.LLMResult{}, permanent(opComplete, ErrBadResponse)
	}
	out, err := values.NewLLMResult(values.NewUntrustedOutput(res.GetOutput()),
		res.GetUsage().GetInputTokens(), res.GetUsage().GetOutputTokens())
	if err != nil {
		return values.LLMResult{}, permanent(opComplete, ErrBadResponse)
	}
	return out, nil
}

// checkBaseURL accepts https://host[:port][/path], or http only for a loopback host.
func checkBaseURL(op, raw string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%s: %w: base url %s", op, ErrInvalidConfig, why)
	}
	u, err := url.Parse(raw)
	switch {
	case raw == "":
		return bad("is empty")
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
	case "http":
		if !isLoopback(u.Hostname()) {
			return bad("must use https")
		}
	default:
		return bad("must use https")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// httpClientFor copies base (or builds a default client) and refuses redirects, so the Authorization header never
// follows one. Timeout is a backstop; each call has its own context deadline.
func httpClientFor(base *http.Client, timeout time.Duration) *http.Client {
	var c http.Client
	if base != nil {
		c = *base
	} else {
		c.Transport = defaultTransport()
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.Timeout == 0 {
		c.Timeout = timeout + clientGrace
	}
	return &c
}

func defaultTransport() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t = &http.Transport{Proxy: http.ProxyFromEnvironment}
	} else {
		t = t.Clone()
	}
	// Student networks often need the environment proxy, so Proxy stays as in the default transport.
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	t.TLSHandshakeTimeout = tlsTimeout
	t.IdleConnTimeout = idleConnLimit
	t.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	return t
}
