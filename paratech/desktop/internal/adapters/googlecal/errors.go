package googlecal

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Reasons of a failed call (ARCH-32). A returned error is an *Error that matches one reason, and
// backoff.ErrTransient or backoff.ErrPermanent, with errors.Is.
var (
	// ErrInvalidConfig is returned by New for a missing dependency or an unusable option.
	ErrInvalidConfig = errors.New("googlecal: invalid configuration")

	// ErrInvalidRequest means the call cannot be made: a nil or deleted lesson, a bad id, a zero command.
	ErrInvalidRequest = errors.New("invalid calendar request")

	// ErrNotSignedIn means there is no Google sign-in (or no OAuth client): the user must connect the calendar. The
	// app maps it to homeworkflow.ErrCalendarNotConnected. ErrReauthRequired matches it too.
	ErrNotSignedIn = errors.New("not signed in to google")

	// ErrReauthRequired means Google no longer accepts the stored sign-in: the user must sign in again.
	ErrReauthRequired = errors.New("google sign-in expired")

	// ErrAuthRejected means the token endpoint refused the refresh for another reason.
	ErrAuthRejected = errors.New("google refused the access token request")

	// ErrUnauthorized means the Calendar API answered 401 twice, also with a refreshed access token.
	ErrUnauthorized = errors.New("google calendar rejected the access token")

	// ErrForeignCalendar means the calendar id is not the stored app calendar (any more); nothing was sent.
	ErrForeignCalendar = errors.New("not the app calendar")

	// ErrCredentials means the credential store failed.
	ErrCredentials = errors.New("calendar id cannot be read or stored")

	// ErrRateLimited means Google asked to slow down (429, or 403 with a rate or quota limit reason).
	ErrRateLimited = errors.New("google calendar rate limit")

	// ErrForbidden means Google refused the request (403 for another reason).
	ErrForbidden = errors.New("google calendar refused the request")

	// ErrNotFound means the calendar or the event does not exist where the call needs it.
	ErrNotFound = errors.New("google calendar resource not found")

	// ErrConflict means the event id stays taken: an insert answered 409 and the update after it did not succeed.
	ErrConflict = errors.New("google calendar event id conflict")

	// ErrRejected means Google refused the request as malformed (400 and other 4xx).
	ErrRejected = errors.New("google calendar rejected the request")

	// ErrUnavailable means Google answered with a server error, or the access token could not be refreshed for a
	// reason that may pass.
	ErrUnavailable = errors.New("google calendar unavailable")

	// ErrTimeout means one request did not finish within the request timeout.
	ErrTimeout = errors.New("google calendar request timed out")

	// ErrNetwork means Google could not be reached. It wraps shared.ErrNetworkUnavailable, so a job waits for the
	// network instead of using attempts.
	ErrNetwork = fmt.Errorf("google calendar cannot be reached: %w", shared.ErrNetworkUnavailable)

	// ErrBadResponse means the answer was malformed, too large or a redirect.
	ErrBadResponse = errors.New("malformed google calendar response")
)

// maxRetryAfter bounds a Retry-After from Google, so a broken value cannot park a job for days.
const maxRetryAfter = time.Hour

// Error is a classified failure of a Calendar API call. Its text is fixed.
type Error struct {
	op         string
	reason     error
	extra      []error // further sentinels for errors.Is, not part of the text
	class      error   // backoff.ErrTransient or backoff.ErrPermanent
	status     int     // HTTP status, 0 without an answer
	retryAfter time.Duration
}

func (e *Error) Error() string {
	if e.status != 0 {
		return e.op + ": " + e.reason.Error() + " (status " + strconv.Itoa(e.status) + ")"
	}
	return e.op + ": " + e.reason.Error()
}

// Unwrap exposes the reason, the extra sentinels and the class to errors.Is.
func (e *Error) Unwrap() []error {
	return append(append([]error{e.reason}, e.extra...), e.class)
}

// RetryAfter is the wait Google asked for, or 0. A use case reads it through errors.As with an anonymous
// interface { RetryAfter() time.Duration } without importing this package.
func (e *Error) RetryAfter() time.Duration { return e.retryAfter }

func transient(op string, reason error, extra ...error) *Error {
	return &Error{op: op, reason: reason, extra: extra, class: backoff.ErrTransient}
}

func permanent(op string, reason error, extra ...error) *Error {
	return &Error{op: op, reason: reason, extra: extra, class: backoff.ErrPermanent}
}

// rateLimitReasons are the 403 reasons Google documents as "retry with backoff" (docs/refs/google-calendar.md).
var rateLimitReasons = map[string]bool{
	"rateLimitExceeded": true, "userRateLimitExceeded": true, "quotaExceeded": true, "dailyLimitExceeded": true,
}

// statusError classifies an answer the call did not expect. reasons are the error reasons of Google's body.
func statusError(op string, status int, reasons []string, retryAfter time.Duration) *Error {
	var e *Error
	switch {
	case status == 429:
		e = transient(op, ErrRateLimited)
	case status == 403 && hasRateLimitReason(reasons):
		e = transient(op, ErrRateLimited)
	case status == 403:
		e = permanent(op, ErrForbidden)
	case status == 401:
		e = permanent(op, ErrUnauthorized)
	case status == 404, status == 410:
		e = permanent(op, ErrNotFound)
	case status == 409, status == 412:
		e = transient(op, ErrConflict)
	case status >= 500:
		e = transient(op, ErrUnavailable)
	case status >= 400:
		e = permanent(op, ErrRejected)
	default: // 1xx, 3xx (redirects are refused) or an unexpected 2xx
		e = transient(op, ErrBadResponse)
	}
	e.status = status
	if e.class == backoff.ErrTransient {
		e.retryAfter = retryAfter
	}
	return e
}

func hasRateLimitReason(reasons []string) bool {
	for _, r := range reasons {
		if rateLimitReasons[r] {
			return true
		}
	}
	return false
}

// parseRetryAfter reads delay-seconds or an HTTP date; anything else, or a past date, is 0.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		if secs > int64(maxRetryAfter/time.Second) {
			return maxRetryAfter
		}
		d = time.Duration(secs) * time.Second
	} else if at, err := http.ParseTime(v); err == nil {
		d = at.Sub(now)
	}
	return max(0, min(d, maxRetryAfter))
}

// AuthFailure is what went wrong in the token source of the Auth client.
type AuthFailure int

const (
	// AuthNone means the error is not a sign-in problem (transport, Google's token endpoint unavailable): retried.
	AuthNone AuthFailure = iota
	// AuthNotSignedIn means there is no sign-in or no configured OAuth client: ErrNotSignedIn.
	AuthNotSignedIn
	// AuthExpired means Google refused the stored refresh token (invalid_grant): ErrReauthRequired.
	AuthExpired
	// AuthRejected means the token endpoint refused the refresh for another reason (for example the client): ErrAuthRejected.
	AuthRejected
)

// AuthClassifier tells sign-in failures of the Auth client's token source apart. The app supplies it by matching the
// googleauth sentinels; this package cannot import googleauth (ARCH-30). It gets the error of the token source
// unwrapped from *url.Error.
type AuthClassifier func(err error) AuthFailure

// authError is the error of a token source failure the classifier recognized, or nil.
func authError(op string, f AuthFailure) *Error {
	switch f {
	case AuthNotSignedIn:
		return permanent(op, ErrNotSignedIn)
	case AuthExpired:
		return permanent(op, ErrReauthRequired, ErrNotSignedIn)
	case AuthRejected:
		return permanent(op, ErrAuthRejected)
	}
	return nil
}
