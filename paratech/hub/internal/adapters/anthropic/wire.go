package anthropic

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"paratech/hub/internal/domain/llm/values"
)

// Response limits. No response size limit is documented; output is at most 128K tokens, far below maxResponseBytes.
const (
	maxResponseBytes    = 8 << 20
	maxErrorBodyBytes   = 64 << 10
	maxRetryAfter       = 10 * time.Minute
	emptyDataText       = "(no data)" // the API rejects an empty or whitespace-only text block
	spendLimitCode      = "enforced_spend_limit_reached"
	contentTypeJSON     = "application/json"
	responseTypeMessage = "message"
)

// errRead marks a failure while reading the body of a 200 answer; Complete reports it as an interrupted answer.
var errRead = errors.New("reading the response failed")

// knownErrorTypes are the error.type values of docs/refs/anthropic.md; any other value is reported as unknown.
var knownErrorTypes = map[string]bool{
	"invalid_request_error": true, "authentication_error": true, "billing_error": true, "permission_error": true,
	"not_found_error": true, "conflict_error": true, "request_too_large": true, "rate_limit_error": true,
	"api_error": true, "timeout_error": true, "overloaded_error": true,
}

// stopReasons maps the documented stop_reason values; others (tool_use, pause_turn, future ones) are StopOther.
var stopReasons = map[string]values.StopReason{
	"end_turn":                      values.StopEnd,
	"max_tokens":                    values.StopMaxTokens,
	"stop_sequence":                 values.StopSequence,
	"refusal":                       values.StopRefusal,
	"model_context_window_exceeded": values.StopContextWindow,
}

type wireRequest struct {
	Model     string        `json:"model"`
	MaxTokens int64         `json:"max_tokens"`
	System    string        `json:"system"`
	Messages  []wireMessage `json:"messages"`
	Stream    bool          `json:"stream,omitempty"`
}

type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// newWireRequest puts the trusted instructions in system and the untrusted data in the only user message, so the
// data is never in the instruction slot. No sampling parameters: current models reject non-default values.
func newWireRequest(req values.Request) wireRequest {
	data := req.Data()
	if strings.TrimSpace(data) == "" {
		data = emptyDataText
	}
	return wireRequest{
		Model:     req.Model(),
		MaxTokens: req.MaxOutput(),
		System:    req.Instructions(),
		Messages:  []wireMessage{{Role: "user", Content: []wireBlock{{Type: "text", Text: data}}}},
	}
}

type wireResponse struct {
	Type       string      `json:"type"`
	Content    []wireBlock `json:"content"`
	StopReason *string     `json:"stop_reason"`
	Usage      *wireUsage  `json:"usage"`
}

type wireUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

type wireError struct {
	Error *struct {
		Type    string `json:"type"`
		Details *struct {
			ErrorCode string `json:"error_code"`
		} `json:"details"`
	} `json:"error"`
}

// readCompletion decodes a 200 answer. A body that cannot be read is errRead; a body that is not a valid message is
// a permanent error (the call may have been billed; the gateway decides what to charge).
func readCompletion(resp *http.Response) (values.Completion, error) {
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != contentTypeJSON {
		return values.Completion{}, permanent("unexpected content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return values.Completion{}, errRead
	}
	if len(body) > maxResponseBytes {
		return values.Completion{}, permanent("response too large")
	}

	var r wireResponse
	if err := json.Unmarshal(body, &r); err != nil || r.Type != responseTypeMessage || r.Usage == nil {
		return values.Completion{}, permanent("malformed response")
	}
	usage, ok := r.Usage.toUsage()
	if !ok {
		return values.Completion{}, permanent("malformed usage")
	}

	var text strings.Builder
	for _, b := range r.Content {
		if b.Type == "text" { // thinking and other blocks are not output
			text.WriteString(b.Text)
		}
	}
	completion, err := values.NewCompletion(text.String(), usage, stopReason(r.StopReason))
	if err != nil {
		return values.Completion{}, permanent("malformed response")
	}
	return completion, nil
}

// stopReason maps a stop_reason; null and unknown values are StopOther.
func stopReason(raw *string) values.StopReason {
	if raw != nil {
		if s, known := stopReasons[*raw]; known {
			return s
		}
	}
	return values.StopOther
}

// toUsage sums all input tokens (base, cache writes, cache reads; nullable cache fields count as 0), as billed.
func (u *wireUsage) toUsage() (values.Usage, bool) {
	if u.InputTokens == nil || u.OutputTokens == nil {
		return values.Usage{}, false
	}
	var input int64
	for _, p := range []*int64{u.InputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens} {
		if p == nil {
			continue
		}
		if *p < 0 || input > math.MaxInt64-*p {
			return values.Usage{}, false
		}
		input += *p
	}
	usage, err := values.NewUsage(input, *u.OutputTokens)
	return usage, err == nil
}

// statusError classifies a non-200 answer by HTTP status, then error.type and error.details.error_code
// (docs/refs/anthropic.md section 4). Only known error types and fixed texts go into the error.
func statusError(resp *http.Response) *values.ProviderError {
	code := resp.StatusCode
	var body wireError
	if raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes)); err == nil {
		_ = json.Unmarshal(raw, &body) // a non-JSON body (proxy page) leaves the type empty
	}
	e := &values.ProviderError{Kind: values.KindPermanent, Status: code}
	spendLimit := false
	if body.Error != nil {
		e.Type = values.TypeUnknown
		if knownErrorTypes[body.Error.Type] {
			e.Type = body.Error.Type
		}
		spendLimit = body.Error.Details != nil && body.Error.Details.ErrorCode == spendLimitCode
	}

	switch {
	case code >= 300 && code < 400:
		e.Reason = "redirect refused"
	case code == http.StatusTooManyRequests && spendLimit:
		e.Reason = "spend limit reached"
	case code == http.StatusTooManyRequests:
		e.Kind, e.Reason = values.KindTransient, "rate limited"
	case code == http.StatusRequestTimeout:
		e.Kind, e.Reason = values.KindTransient, "request timeout"
	case code == 529:
		e.Kind, e.Reason = values.KindTransient, "overloaded"
	case code >= 500 && code < 600:
		e.Kind, e.Reason = values.KindTransient, "server error"
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		e.Reason = "credentials rejected"
	case code == http.StatusPaymentRequired:
		e.Reason = "billing problem"
	case code == http.StatusRequestEntityTooLarge:
		e.Reason = "request too large"
	case code >= 400 && code < 500:
		e.Reason = "request rejected"
	default:
		e.Reason = "unexpected status"
	}
	if e.Kind == values.KindTransient {
		e.RetryAfter = retryAfter(resp.Header.Get("retry-after"))
	}
	return e
}

// retryAfter reads the retry-after header in seconds (the documented form), bounded by maxRetryAfter; anything else
// is 0.
func retryAfter(v string) time.Duration {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n > int64(maxRetryAfter/time.Second) {
		return maxRetryAfter
	}
	return time.Duration(n) * time.Second
}
