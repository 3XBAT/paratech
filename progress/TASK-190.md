# TASK-190 — provider_stream on by default, JSON fallback

## Done
- `hub/config`: `ProviderStream` defaults to true; `HUB_PROVIDER_STREAM=false` / `-provider-stream=false` turn it off.
- `anthropic.Client.Stream`: a 200 `application/json` answer to a streaming request is read via the Complete path
  and sent as one delta (none if empty), usage and stop as Complete; one upstream call. Other non-SSE answers stay
  provider errors. Log line «provider answered without SSE» once per client (one client per process), no texts.
- `Options.Log` added and wired in `hub/internal/app/app.go` (`newLLMParts` now takes the logger) after review
  finding ARCH-53 (no package-level mutable state, no `slog.Default`). Files outside the task list: `client.go`, `app.go`.
- README and ADR-0026 addendum (supersedes open question 5; TASK-145 note on deltas vs stored result).

## Changed existing tests
- `config/stream_test.go`, `config/config_test.go`: default flipped to true.
- `anthropic/stream_test.go` `TestStreamRejectsMalformedStreams`: the JSON row replaced by HTML and plain-text rows
  (JSON is now the supported fallback).

## Decisions to confirm
- «Once per process» is implemented as once per client (same in practice).
- The log fires before the JSON is parsed, so a malformed JSON answer also uses it up.

## Follow-ups
- ADR-0026 «Flag» bullet and the comment on `ProviderStream` in `cases/gateway/types.go` still say default false.
- No end-to-end JSON-over-stream test through the gateway (testenv has no streaming fake).
- Postgres-backed `./hub/...` tests were not run locally (CI).

## Security
security-auditor: no findings. Reviewer: one blocker (ARCH-53), fixed in ed323acb and re-approved.
