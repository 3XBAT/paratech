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
//     key never goes to another host. No URL, a refused stored URL or no key is "hub: not configured", which waits
//     like a network failure.
//
// Step runs one agent planner step (GatewayService.AgentStepStream, falling back to AgentStep on an older Hub;
// ADR-0023, ADR-0026; the agentturn.AgentModel port) with the same URL, key, limits and error classes. Its key
// ag:<thread>:<turn>:<step> comes from the agent thread. The Hub's failed_precondition without a detail («the model
// turn is gone») is ErrTurnGone, which also matches the agent domain's values.ErrTurnGone. The streamed reply text is
// only handed to the caller's draft, bounded and cleaned (replydraft.go).
//
// InspectMaterial runs the quarantined reader of a library material (OPERATION_TYPE_INSPECT_MATERIAL, ADR-0022) with
// the same URL, key, limits and error classes; it sends no instructions or alias (the Hub's own prompt), and its output
// is parsed only by inspection.Request.Parse.
//
// Summarize asks for the summary of a chat's earlier requests (OPERATION_TYPE_CHAT_SUMMARY, ADR-0028; the
// agentcompact.Summarizer port), unary, with the same URL, key, limits and error classes and the key cs:<thread>:<through>:<hash8>;
// it sends no instructions, alias or output cap, and only an answer exactly {"summary":"<text>"} is returned.
//
// GetRemaining reads the tester's quota (QuotaService.GetRemaining, AC-12) with the same URL, key, limits and error
// classes, for the «Подключения» page; the answer is shown, never used to decide (the Hub decides at Reserve).
//
// Retries live in the job runner, not here: an error is classified (backoff.Transient or backoff.Permanent, see
// errors.go) and the runner retries with its bounded exponential backoff, like every other handler (TASK-009). One
// call is one attempt bounded by Config.RequestTimeout. The two streams (a streamed agent step and AnswerQuestion) ask
// for the Hub's heartbeat and are bounded instead by the Hub's silence, the same watchdog for both (stream_watch.go):
// RequestTimeout before the first message, Config.StepIdleTimeout after one, any message (a heartbeat too) resets it;
// overall Config.StepTimeout or Config.AnswerTimeout. shared.ErrNetworkUnavailable and shared.ErrQuotaExceeded tell
// the caller to park the job in WAITING_FOR_NETWORK or WAITING_FOR_QUOTA (PRD section 7.2). A Hub retry_after is
// exposed by the RetryAfter method of *Error.
//
// Error texts are fixed: they never contain the API key, the request, or text from the Hub or the provider.
package hubclient
