package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

// Operation names carried by the errors of the adapter.
const (
	opNew      = "anthropic.New"
	opComplete = "anthropic.Complete"
	opStep     = "anthropic.Step"
	opStream   = "anthropic.Stream"
)

const (
	messagesPath = "/v1/messages"
	apiVersion   = "2023-06-01"
)

// ErrInvalidOptions means New got options it cannot work with.
var ErrInvalidOptions = errors.New("invalid anthropic options")

// Options configure a Client.
type Options struct {
	// APIKey is the provider key from the server configuration (secret).
	APIKey string
	// BaseURL is the absolute http(s) URL of the API without path, query or user info, e.g. https://api.anthropic.com.
	BaseURL string
	// Timeout bounds one call, on top of the caller's context.
	Timeout time.Duration
	// Transport replaces http.DefaultTransport (tests). The adapter builds its own http.Client around it so that
	// redirects are always refused.
	Transport http.RoundTripper
}

// String prints the options without the key.
func (o Options) String() string {
	return fmt.Sprintf("{APIKey:%s BaseURL:%s Timeout:%s}", redacted(o.APIKey), o.BaseURL, o.Timeout)
}

// GoString is String for %#v.
func (o Options) GoString() string { return "anthropic.Options" + o.String() }

// LogValue is String for slog handlers that do not use fmt.
func (o Options) LogValue() slog.Value {
	return slog.GroupValue(slog.String("api_key", redacted(o.APIKey)), slog.String("base_url", o.BaseURL),
		slog.Duration("timeout", o.Timeout))
}

// Client calls the Messages API. It is safe for concurrent use.
type Client struct {
	key      string
	endpoint string
	timeout  time.Duration
	http     *http.Client
}

// New validates o and returns a client.
func New(o Options) (*Client, error) {
	if !validKey(o.APIKey) {
		return nil, fmt.Errorf("%s: %w: the API key must be non-empty printable ASCII without spaces", opNew, ErrInvalidOptions)
	}
	if o.Timeout <= 0 {
		return nil, fmt.Errorf("%s: %w: the timeout must be above 0", opNew, ErrInvalidOptions)
	}
	endpoint, err := messagesURL(o.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", opNew, ErrInvalidOptions, err.Error())
	}
	transport := o.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{
		key:      o.APIKey,
		endpoint: endpoint,
		timeout:  o.Timeout,
		http: &http.Client{
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// String prints the client without the key. Value receivers, so a dereferenced client is redacted too.
func (c Client) String() string {
	return fmt.Sprintf("anthropic.Client{Endpoint:%s Timeout:%s APIKey:%s}", c.endpoint, c.timeout, redacted(c.key))
}

// GoString is String for %#v.
func (c Client) GoString() string { return c.String() }

// LogValue is String for slog handlers that do not use fmt.
func (c Client) LogValue() slog.Value {
	return slog.GroupValue(slog.String("endpoint", c.endpoint), slog.Duration("timeout", c.timeout),
		slog.String("api_key", redacted(c.key)))
}

// Complete sends one Messages API call. The deadline is the earlier of ctx and the client timeout. A failure is a
// *values.ProviderError (see the gateway's Provider port): the client's own timeout and network failures are
// transient without a cause, and so is a 200 answer cut off mid-body ("answer interrupted", probably billed); when
// ctx ended the call the error is transient and also matches ctx.Err().
func (c *Client) Complete(ctx context.Context, req values.Request) (values.Completion, error) {
	if req.Model() == "" || req.Instructions() == "" || req.MaxOutput() <= 0 {
		return values.Completion{}, fail(permanent("invalid request"))
	}
	body, err := json.Marshal(newWireRequest(req))
	if err != nil {
		return values.Completion{}, fail(permanent("request encoding failed"))
	}
	var completion values.Completion
	err = c.post(ctx, body, contentTypeJSON, func(resp *http.Response) error {
		var rerr error
		completion, rerr = readCompletion(resp)
		return rerr
	})
	if err != nil {
		return values.Completion{}, fail(err)
	}
	return completion, nil
}

// Step sends one planner call with the catalog tools and tool choice "auto" (ADR-0023), with the same deadline and
// error rules as Complete. The answer's calls are untrusted; whether they are usable is the use case's decision.
func (c *Client) Step(ctx context.Context, call values.AgentCall) (values.AgentAnswer, error) {
	if call.Model() == "" || call.System() == "" || call.MaxOutput() <= 0 || len(call.Tools()) == 0 || len(call.Turns()) == 0 {
		return values.AgentAnswer{}, failStep(permanent("invalid request"))
	}
	body, err := json.Marshal(newAgentWireRequest(call))
	if err != nil {
		return values.AgentAnswer{}, failStep(permanent("request encoding failed"))
	}
	var answer values.AgentAnswer
	err = c.post(ctx, body, contentTypeJSON, func(resp *http.Response) error {
		var rerr error
		answer, rerr = readAgentAnswer(resp)
		return rerr
	})
	if err != nil {
		return values.AgentAnswer{}, failStep(err)
	}
	return answer, nil
}

// post sends body to the Messages endpoint and hands a 200 answer to read. The deadline is the earlier of ctx and the
// client timeout, for the whole body too. Every failure is a *values.ProviderError.
func (c *Client) post(ctx context.Context, body []byte, accept string, read func(*http.Response) error) error {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return permanent("request build failed")
	}
	httpReq.Header.Set("x-api-key", c.key)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", accept)

	// The endpoint is fixed at New from the server configuration; no request value reaches the URL.
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return transportError(ctx, callCtx)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	perr := read(resp)
	if errors.Is(perr, errRead) {
		return interruptedError(ctx, callCtx)
	}
	return perr
}

// fail adds the operation name; errors.Is/As still see the ProviderError.
func fail(err error) error { return fmt.Errorf("%s: %w", opComplete, err) }

// failStep is fail for Step.
func failStep(err error) error { return fmt.Errorf("%s: %w", opStep, err) }

// failStream is fail for Stream.
func failStream(err error) error { return fmt.Errorf("%s: %w", opStream, err) }

// transportError classifies a call that got no complete answer: ctx ended it, the client timeout fired, or the
// network failed. The raw error is dropped (ARCH-32).
func transportError(ctx, callCtx context.Context) *values.ProviderError {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return &values.ProviderError{Kind: values.KindTransient, Reason: "cancelled", Cause: ctx.Err()}
	case ctx.Err() != nil:
		return &values.ProviderError{Kind: values.KindTransient, Reason: "deadline exceeded", Cause: ctx.Err()}
	case callCtx.Err() != nil:
		return &values.ProviderError{Kind: values.KindTransient, Reason: "timeout"}
	default:
		return &values.ProviderError{Kind: values.KindTransient, Reason: "connection failed"}
	}
}

// interruptedError classifies a 200 answer whose body broke off (connection cut, client timeout mid-body). The
// provider has probably billed the call, so it gets its own reason; when ctx ended the call, ctx wins as above.
func interruptedError(ctx, callCtx context.Context) *values.ProviderError {
	if ctx.Err() != nil {
		return transportError(ctx, callCtx)
	}
	return &values.ProviderError{Kind: values.KindTransient, Reason: "answer interrupted"}
}

func permanent(reason string) *values.ProviderError {
	return &values.ProviderError{Kind: values.KindPermanent, Reason: reason}
}

// messagesURL builds the endpoint from the base URL. Messages never quote the URL (it could hold user info).
func messagesURL(base string) (string, error) {
	u, err := url.Parse(base)
	switch {
	case err != nil:
		return "", errors.New("the base URL is not a valid URL")
	case u.Scheme != "https" && u.Scheme != "http":
		return "", errors.New("the base URL scheme must be https or http")
	case u.Host == "" || u.Opaque != "":
		return "", errors.New("the base URL must be absolute with a host")
	case u.User != nil:
		return "", errors.New("the base URL must not contain user info")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", errors.New("the base URL must not contain a query or fragment")
	case u.Path != "" && u.Path != "/":
		return "", errors.New("the base URL must not contain a path")
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: messagesPath}).String(), nil
}

// validKey accepts printable ASCII without spaces, so the key is a valid header value.
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

func redacted(secret string) string {
	if secret == "" {
		return "(unset)"
	}
	return "(redacted)"
}
