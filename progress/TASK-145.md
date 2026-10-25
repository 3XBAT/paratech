# TASK-145: Hub CompleteStream for ANSWER

Status: done. Branch ralph/20261025-task-145.

## What was done
- `GatewayService.CompleteStream` serves only `OPERATION_TYPE_ANSWER` (others `invalid_argument`, checked before
  limiter and ledger). `Complete` and `CompleteStream` share one `run` path: validation, server prompt, fingerprint,
  key life cycle, settle, stored result. They replay each other's result under the same key and spec; a replay is
  one delta then the result.
- Port `StreamingProvider` in `cases/gateway`. Flag `provider_stream` (env `HUB_PROVIDER_STREAM`, flag
  `-provider-stream`, default OFF). Off: `Provider.Complete`, the checked output goes as one chunk after settlement.
  On: `StreamingProvider.Stream`, raw chunks pass through.
- Provider call on `context.WithoutCancel` bounded by the call timeout: a vanished client does not abort it; the Hub
  reads to the end, settles once in full, stores the result; the next call with the key replays free.
- No retry exists, so Stream is never repeated after a delta. Provider failure (error event, cut stream, refusal,
  error first) -> PROVIDER_ERROR, reservation released, no delta after the error. A billed answer breaking the
  schema is charged and stored as the failure marker (ADR-0023), replayed as a refusal by both RPCs.
- Handler `deltaSender` sends from its own goroutine so a slow client cannot hold the provider call.
- Logs carry counts only (`stream`, `provider_stream`, `chunks`), no texts.
- Proto: comments only; regenerated `gateway.connect.go` (comment-only diff, needed by the ARCH-35 drift check).
- Test `TestStreamErrorAsTheFirstEvent` (adapters/anthropic); `docs/refs/anthropic.md` §8: Stream keeps text +
  usage only; also fixed the preceding paragraph (interrupted stream is released, not settled).

## Decisions to confirm
- Contract change: deltas are untrusted draft text; `CompletionResult.output` is the checked canonical answer and may
  differ from the joined deltas (e.g. no ```json fence). Old comment promised equality. Flag off and replay: equal.
  Needs a short ADR-0026 addendum (not written, docs/adr not in the task's file list). TASK-146:
  `answer.Draft(partial)` must tolerate a leading code fence or prose.
- Files outside the task list: `shared/gen/.../gateway.connect.go` (generated), `adapters/anthropic/stream_test.go`,
  `docs/refs/anthropic.md`, README (table row + paragraph instead of one line).
- `Completer` interface kept; CompleteStream is mounted through option `WithCompleteStream` (no option -> unimplemented).
- Open from ADR-0026: who runs the gateway probe / may `provider_stream` be on for beta testers: owner's call.

## Existing tests changed
- `TestCompleteStreamIsUnimplemented` (connect) replaced by behaviour tests; unimplemented-without-option moved into
  `TestGatewayExposure`.
- `hub/tests/keys_test.go`: CompleteStream with valid key and empty spec now expects invalid_argument (RPC is served).

## Follow-ups (lows from review/audit)
- service.go ~line 288 comment "Nothing of the answer leaves the Hub" is stale with the flag on (drafts of an invalid
  answer may have been streamed); reword.
- Security low: with the flag on, text streamed before a refusal/interruption is released, not charged (accepted for
  Complete in ADR-0005(e)/0010 but there nothing reached the client). Accept in ADR-0026 or settle usage when
  chunks > 0. Max one call's output.
- A client that stops reading can hold a handler goroutine (no WriteTimeout); consider per-stream write deadline
  and a `deltaSender` test with a blocking Send.
- `testenv/messagesapi.go` has no streaming fake; no end-to-end SSE test through the full app.

## Verification
go vet and `go test -race` of ./hub/... ./shared/... green; golangci-lint v2.14.0 0 issues; buf generate/lint/format/
breaking green. Postgres tests skipped (HUB_TEST_DATABASE_URL unset, as in CI here); the `keys_test.go` change is
verified only by CI.

## Security
Auditor: no high/medium; one low (above). Reviewer: approve, no blockers.
