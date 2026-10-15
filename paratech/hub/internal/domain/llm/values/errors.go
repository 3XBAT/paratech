package values

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Errors of the llm values (ARCH-18).
var (
	// ErrInvalidRequest means a provider request part breaks its rule.
	ErrInvalidRequest = errors.New("invalid provider request")

	// ErrInvalidCompletion means a provider answer breaks a rule (negative usage, unknown stop reason).
	ErrInvalidCompletion = errors.New("invalid completion")

	// ErrProviderTransient matches a ProviderError for which repeating the same call later can help (rate limit,
	// overload, server error, timeout, network failure).
	ErrProviderTransient = errors.New("transient provider error")

	// ErrProviderPermanent matches a ProviderError for which repeating the same call cannot help (bad request,
	// rejected credentials, spend cap, malformed answer).
	ErrProviderPermanent = errors.New("permanent provider error")
)

// ErrorKind classifies a provider failure (ADR-0005(c)). The zero value is KindPermanent, so an unclassified failure
// is never retried.
type ErrorKind uint8

// Error kinds.
const (
	KindPermanent ErrorKind = iota
	KindTransient
)

// String names the kind.
func (k ErrorKind) String() string {
	if k == KindTransient {
		return "transient"
	}
	return "permanent"
}

// TypeUnknown is the Type of a provider error type the adapter does not know.
const TypeUnknown = "unknown"

// maxTypeLen bounds the printed provider error type.
const maxTypeLen = 64

// ProviderError is a classified provider failure. errors.Is matches ErrProviderTransient or ErrProviderPermanent by
// Kind, and Cause when set. It carries no provider message, prompt or output text: adapters fill it only with fixed
// texts and known values.
type ProviderError struct {
	Kind ErrorKind
	// Reason is a fixed text chosen by the adapter (for example "timeout", "rate limited"); never provider text.
	Reason string
	// Status is the HTTP status of the provider answer, 0 when there was none (network failure, timeout).
	Status int
	// Type is the provider's error type when the adapter knows the value, TypeUnknown otherwise, empty when none.
	Type string
	// RetryAfter is the wait the provider asked for, 0 when not given.
	RetryAfter time.Duration
	// Cause is context.Canceled or context.DeadlineExceeded when the caller's context ended the call, else nil.
	Cause error
}

// Error implements error.
func (e *ProviderError) Error() string {
	var b strings.Builder
	b.WriteString("provider error (" + e.Kind.String() + ")")
	if e.Reason != "" {
		b.WriteString(": " + e.Reason)
	}
	if e.Status != 0 {
		b.WriteString(", status " + strconv.Itoa(e.Status))
	}
	if e.Type != "" {
		b.WriteString(", type " + safeType(e.Type))
	}
	if e.RetryAfter > 0 {
		b.WriteString(", retry after " + e.RetryAfter.String())
	}
	return b.String()
}

// Unwrap lets errors.Is see the sentinel of Kind and the Cause.
func (e *ProviderError) Unwrap() []error {
	sentinel := ErrProviderPermanent
	if e.Kind == KindTransient {
		sentinel = ErrProviderTransient
	}
	if e.Cause == nil {
		return []error{sentinel}
	}
	return []error{sentinel, e.Cause}
}

// safeType prints only identifier-like types, as a second line of defence against provider text in logs.
func safeType(t string) string {
	if len(t) > maxTypeLen {
		return TypeUnknown
	}
	for i := 0; i < len(t); i++ {
		if c := t[i]; (c < 'a' || c > 'z') && c != '_' {
			return TypeUnknown
		}
	}
	return t
}
