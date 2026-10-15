# ADR-0009: Hub LLM gateway

- **Status:** Accepted
- **Source:** TASK-035; builds on ADR-0005 (contract) and ADR-0008 (quota ledger)

## Context
`GatewayService.Complete` must validate the request, rate limit the tester, reserve quota, call the provider, settle or release,
and replay a completed answer for the same idempotency key (gateway.proto). ADR-0008 left three points to this task: the request
hash, an upper-bound reservation and a deadline below the reservation TTL.

## Decision
- **Order of a call.** Auth (middleware) -> rate limit -> validation -> `Start` (one ledger transaction) -> provider -> `Complete`
  (settle + store, one transaction) or `Release`. A rate-limited or invalid call writes nothing.
- **Validation** lives in `domain/llm/values.Spec` (ADR-0005(b) rules, 1 MiB payload) and the allowlist (`modelallowlist`, built
  in `app.New`; a bad entry or a missing `HUB_ANTHROPIC_API_KEY` stops `serve`, the key commands do not need the key).
- **Reservation** (`domain/llm/services/requestlimits`) = bytes of instructions + data + 1024 framing tokens + the clamped
  `max_output_tokens`. A provider token is at least one byte, so this bounds the charge without a tokenizer; it errs high.
- **Key record.** The ledger fingerprint gets `request_hash`: SHA-256 of operation, alias, both texts and the requested
  `max_output_tokens` (each length-prefixed, the key itself left out). A different hash is IDEMPOTENCY_CONFLICT in every state,
  checked before anything is replayed. The answer (output + usage) is stored on the key record by `account.Complete` in the
  settle transaction, so one aggregate per transaction still holds (ARCH-23). Output is opaque bytes to the quota context. A late
  attempt (a newer one owns the key) is charged but its result is dropped. Migration 0004 adds the columns; no request text is
  stored.
- **Retention and purge.** A stored result is never returned once its key is past `HUB_IDEMPOTENCY_RETENTION` (same start as
  the ledger key). `app` runs a purge at start and every 5 minutes that clears results of such keys, so output stays in the
  database at most about 5 minutes past the retention (longer only while the Hub is down); the rows stay without output.
- **Deadlines.** The provider call runs on `context.WithoutCancel` with `min(HUB_LLM_TIMEOUT, expires_at - now - 5s)`; settle and
  release run detached with 5 s each. Config already keeps `2 x HUB_LLM_TIMEOUT <= TTL`. A client that leaves before the call gets
  its hold released (reason `explicit`); after the call started, the call finishes, is settled and stored, and a retry replays it.
- **No retries in the Hub.** The desktop job queue retries with backoff; `retry_after` passes the provider's wait on.
- **Rate limit** per API key: GCRA token bucket (`requestlimits.Rate`, std only), state in `adapters/ratelimit` (in memory, idle
  states dropped). `HUB_RATE_LIMIT_PER_MINUTE` (30) and `HUB_RATE_LIMIT_BURST` (10). Over the limit: RATE_LIMITED, retryable.
- **Errors** (`input/connect/gateway.go`, ADR-0005(c)): fixed client texts only. Provider 401/403 is PROVIDER_ERROR (permanent),
  never INVALID_API_KEY; a refusal is PROVIDER_ERROR (permanent); settle failures and anything unknown are `internal`.
- **Logs:** one line per call (outcome, operation, alias, reservation, token counts, duration); never texts, output or keys.
- `CompleteStream` stays unimplemented: the provider port has no streaming yet.

## Accepted gaps
- A provider failure after it generated output ("answer interrupted", network error after send, refusal) is released, not
  charged (ADR-0005(e)). Bounded by the rate limit and quota.
- If the settle transaction fails, the hold expires uncharged at `expires_at`; the retry is `aborted` until then.
- Rate limit state is per process and lost on restart; anonymous traffic is stopped by auth, not by this limit.
- Changing a model cap between attempts changes the reserved amount, so a retry of the same request conflicts.

## Consequences
- Follow-ups: streaming (`CompleteStream`, partial settle); a per-key limit of concurrent calls; a sweeper that deletes stale key
  rows (ADR-0008); limiting failed auth attempts per client address.
