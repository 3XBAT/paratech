# TASK-144: Anthropic streaming in the provider adapter, with a gateway probe

- **Agent:** hub-dev (opus)
- **Label:** hub, spike
- **Depends on:** -
- **PRD sections / AC:** §4.2, §9.2; ADR-0010, ADR-0023, ADR-0026 «Streaming»; hard rule 9
- **Branch:** ralph/<date>-task-144

## Goal
The anthropic adapter can run one text call with `stream: true` and report deltas and usage; a probe tells whether
the owner's gateway (`ai.starimg.ru`) passes the stream through.

## Allowed to change
- `docs/refs/anthropic.md` (new streaming section from the official docs: events `message_start`,
  `content_block_start/delta/stop` with `text_delta`, `message_delta` with usage and `stop_reason`, `message_stop`,
  `ping`, `error`; headers; how usage is reported)
- `paratech/hub/internal/adapters/anthropic/*` (new `stream.go`, `stream_test.go`, `probe_test.go`)

## Out of scope
- The gateway use case, Connect handler and config flag (TASK-145); tool-use streaming (`input_json_delta`).

## Acceptance criteria
- [ ] `(*Client).Stream(ctx, req values.Request, onDelta func(chunk string)) (values.Completion, error)` with the
      signature of ADR-0026: same request body as `Complete` plus `stream: true`; SSE read line by line with a bounded
      line and total size; `onDelta` in order; the result output equals the concatenation; usage from `message_start`
      and `message_delta`; `stop_reason` handled as in `Complete`.
- [ ] Errors match `Complete`'s classes (`values.ErrProviderTransient`/`Permanent`, ctx); an `error` event or a stream
      that ends without `message_stop` is a provider error; unknown event types are ignored.
- [ ] Tests on recorded SSE fixtures: normal, split chunks, ping, error mid-stream, truncated stream, oversize line,
      unknown event, `max_tokens` stop; `-race`.
- [ ] `probe_test.go` runs only when `PARATECH_PROBE_ANTHROPIC_URL` and `PARATECH_PROBE_ANTHROPIC_KEY` are set
      (skipped in CI): one short streamed call, asserts ≥ 2 deltas before the end and usage present, logs only event
      names and timings, never the key or the text.
- [ ] `docs/qa/tasks/TASK-144.md`: owner's steps to run the probe; result recorded in `progress/TASK-144.md`.
- [ ] `go vet`, `go test -race ./hub/internal/adapters/anthropic/...`, `golangci-lint` pass.

## Notes
No SDK; the adapter is plain `net/http` (check `wire.go`). No new dependency. If the owner's gateway buffers the
stream, the probe fails and `provider_stream` stays off (fallback in TASK-145).
