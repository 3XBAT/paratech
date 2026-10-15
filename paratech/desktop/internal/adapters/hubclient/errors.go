package hubclient

import (
	"errors"
	"net"
	"time"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

// Reasons of a failed call (ARCH-32). A returned error is an *Error that matches exactly one of them (ErrNotConfigured
// also its reason), or shared.ErrNetworkUnavailable or shared.ErrQuotaExceeded, with errors.Is.
var (
	// ErrInvalidConfig is returned by New for an unusable configuration. A call matches it with ErrNotConfigured when
	// the URL source gives a URL the client refuses.
	ErrInvalidConfig = errors.New("hubclient: invalid configuration")

	// ErrInvalidRequest means the request is the zero value or names an operation the contract does not know.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrNotConfigured means the Hub URL or the API key is not set, or the stored URL is refused. The error also
	// matches the reason (ErrNoURL, ErrMissingAPIKey or ErrInvalidConfig) and shared.ErrNetworkUnavailable, so the job
	// waits in WAITING_FOR_NETWORK instead of failing.
	ErrNotConfigured = errors.New("not configured")

	// ErrNoURL means no Hub URL is set in the settings.
	ErrNoURL = errors.New("hub url is not configured")

	// ErrMissingAPIKey means no Hub API key is stored; the user must enter one (PRD section 12).
	ErrMissingAPIKey = errors.New("hub api key is not configured")

	// ErrSettings means the Hub URL could not be read from the settings.
	ErrSettings = errors.New("hub settings cannot be read")

	// ErrMalformedAPIKey means the stored key cannot be sent in a header (empty, too long, spaces or control
	// characters).
	ErrMalformedAPIKey = errors.New("hub api key is malformed")

	// ErrCredentials means the credential store could not be read.
	ErrCredentials = errors.New("hub api key cannot be read")

	// ErrInvalidAPIKey means the Hub rejected the key (INVALID_API_KEY or unauthenticated).
	ErrInvalidAPIKey = errors.New("hub api key was rejected")

	// ErrRateLimited means the Hub asked to slow down (RATE_LIMITED, or resource_exhausted without a detail).
	ErrRateLimited = errors.New("hub rate limit")

	// ErrProvider means the upstream LLM provider failed (PROVIDER_ERROR); ErrorDetail.retryable decides the class.
	ErrProvider = errors.New("llm provider failed")

	// ErrIdempotencyConflict means the key was used with another request: a bug of the caller.
	ErrIdempotencyConflict = errors.New("idempotency key reused with another request")

	// ErrBusy means an attempt with the same key is still running on the Hub (aborted).
	ErrBusy = errors.New("hub is still running this operation")

	// ErrUnavailable means the Hub or a proxy answered unavailable without a classified reason.
	ErrUnavailable = errors.New("hub unavailable")

	// ErrTimeout means the call did not finish within Config.RequestTimeout.
	ErrTimeout = errors.New("hub request timed out")

	// ErrRejected means the Hub refused the request for a reason retrying cannot fix (invalid_argument and others).
	ErrRejected = errors.New("hub rejected the request")

	// ErrBadResponse means the answer was malformed, too large or a redirect.
	ErrBadResponse = errors.New("malformed hub response")
)

// maxRetryAfter bounds a retry_after from the Hub, so a broken value cannot park a job for days.
const maxRetryAfter = time.Hour

// Error is a classified failure of a Hub call. Its text is fixed; errors.Is matches the reason, the domain sentinel
// (network or quota) when there is one, and the backoff class marker.
type Error struct {
	op         string
	reason     error
	extra      []error // further sentinels for errors.Is, not part of the text
	class      error   // backoff.ErrTransient, backoff.ErrPermanent or nil
	retryAfter time.Duration
}

func (e *Error) Error() string { return e.op + ": " + e.reason.Error() }

// Unwrap exposes the reason, the extra sentinels and the class to errors.Is.
func (e *Error) Unwrap() []error {
	out := append([]error{e.reason}, e.extra...)
	if e.class != nil {
		out = append(out, e.class)
	}
	return out
}

// RetryAfter is the wait the Hub asked for, or 0. A use case can read it through errors.As with an anonymous
// interface { RetryAfter() time.Duration } without importing this package.
func (e *Error) RetryAfter() time.Duration { return e.retryAfter }

func transient(op string, reason error) *Error {
	return &Error{op: op, reason: reason, class: backoff.ErrTransient}
}

func permanent(op string, reason error) *Error {
	return &Error{op: op, reason: reason, class: backoff.ErrPermanent}
}

// notConfigured is the fixed "hub: not configured" of a missing or refused URL or a missing key. It waits like a network failure: the job
// parks without using attempts and runs once the user configures the Hub, while a permanent failure would block the
// job keys of the post-processing for ever.
func notConfigured(why error) *Error {
	return &Error{op: "hub", reason: ErrNotConfigured, extra: []error{why, shared.ErrNetworkUnavailable}, class: backoff.ErrTransient}
}

// quotaExceeded carries no class marker: the job waits for quota, not for a backoff delay. Unmarked errors are
// transient for backoff.Classify, so a runner that does not park jobs yet still stops at its attempt cap.
func quotaExceeded(op string) *Error { return &Error{op: op, reason: shared.ErrQuotaExceeded} }

// mapCallError classifies an error of the generated client by ErrorDetail first and by the Connect code when the
// detail is missing (errors.proto, ADR-0005(c)). The caller has already handled a done parent context.
func mapCallError(op string, err error) *Error {
	ce := new(connect.Error)
	if !errors.As(err, &ce) {
		return transient(op, ErrUnavailable)
	}

	if d := errorDetail(ce); d != nil {
		if e := mapDetail(op, d); e != nil {
			return e
		}
	}

	switch ce.Code() {
	case connect.CodeDeadlineExceeded:
		return transient(op, ErrTimeout)
	case connect.CodeUnavailable:
		// A net.Error under a client-made code means no answer came at all (DNS, dial, reset). Connect strips the
		// *url.Error of http.Client.Do, and a proxy error page carries no net.Error.
		var netErr net.Error
		if !connect.IsWireError(err) && errors.As(err, &netErr) {
			return transient(op, shared.ErrNetworkUnavailable)
		}
		return transient(op, ErrUnavailable)
	case connect.CodeResourceExhausted:
		// A client-side resource_exhausted is the read limit on the answer; only the Hub's own means rate limited.
		if !connect.IsWireError(err) {
			return permanent(op, ErrBadResponse)
		}
		return transient(op, ErrRateLimited)
	case connect.CodeUnauthenticated:
		return permanent(op, ErrInvalidAPIKey)
	case connect.CodeAborted:
		return transient(op, ErrBusy)
	case connect.CodeCanceled:
		// The parent context is not done (checked by the caller), so the stream was reset by the other side.
		return transient(op, ErrUnavailable)
	}
	return permanent(op, ErrRejected)
}

// mapDetail returns nil for a detail without a known code, so the Connect code decides.
func mapDetail(op string, d *hubv1.ErrorDetail) *Error {
	switch d.GetCode() {
	case hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED:
		return quotaExceeded(op)
	case hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED:
		e := transient(op, ErrRateLimited)
		e.retryAfter = retryAfter(d)
		return e
	case hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY:
		return permanent(op, ErrInvalidAPIKey)
	case hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR:
		if !d.GetRetryable() {
			return permanent(op, ErrProvider)
		}
		e := transient(op, ErrProvider)
		e.retryAfter = retryAfter(d)
		return e
	case hubv1.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT:
		return permanent(op, ErrIdempotencyConflict)
	}
	return nil
}

// errorDetail returns the first ErrorDetail of the error, skipping details of other types.
func errorDetail(ce *connect.Error) *hubv1.ErrorDetail {
	for _, raw := range ce.Details() {
		v, err := raw.Value()
		if err != nil {
			continue
		}
		if d, ok := v.(*hubv1.ErrorDetail); ok {
			return d
		}
	}
	return nil
}

func retryAfter(d *hubv1.ErrorDetail) time.Duration {
	ra := d.GetRetryAfter()
	if ra == nil || ra.CheckValid() != nil {
		return 0
	}
	return min(max(ra.AsDuration(), 0), maxRetryAfter)
}
