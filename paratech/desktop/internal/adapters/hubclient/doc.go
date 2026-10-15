// Package hubclient is the desktop client of the cloud LLM Hub (PRD sections 4.2, 4.4, 9.2 and 12). It satisfies the
// LLMClient port of homeworkflow structurally with the generated Connect client of paratech/shared (ADR-0005).
//
//   - Only the fields of CompletionSpec are sent: operation, model alias, instructions, untrusted data and the
//     idempotency key of the request value object. Raw audio, file paths and ids never are (AC-06).
//   - The idempotency key is values.LLMRequest.IdempotencyKey, chosen by the caller and stable across retries of a job;
//     the client never invents one per attempt, so a retry is not charged twice (AC-11).
//   - The per-tester API key is read from the credential store on every call and sent only as
//     "Authorization: Bearer <key>" (common.proto). A Go string cannot be wiped; the copy lives for one call.
//   - The Hub URL comes from the local settings (Config.URLs, read on every call, so a change needs no restart) or a
//     fixed Config.BaseURL: https, or plain http to a loopback host for local tests. Redirects are not followed, so the
//     key never goes to another host. No URL or no key is "hub: not configured", which waits like a network failure.
//
// Retries live in the job runner, not here: an error is classified (backoff.Transient or backoff.Permanent, see
// errors.go) and the runner retries with its bounded exponential backoff, like every other handler (TASK-009). One
// call is one attempt bounded by Config.RequestTimeout. shared.ErrNetworkUnavailable and shared.ErrQuotaExceeded tell
// the caller to park the job in WAITING_FOR_NETWORK or WAITING_FOR_QUOTA (PRD section 7.2). A Hub retry_after is
// exposed by the RetryAfter method of *Error.
//
// Error texts are fixed: they never contain the API key, the request, or text from the Hub or the provider.
package hubclient
