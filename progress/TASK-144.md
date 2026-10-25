# TASK-144: Anthropic streaming in the adapter + gateway probe

## Done
- docs/refs/anthropic.md section 8 «Streaming» was written by the supervisor from the official docs (commit a7979e82).
- `(*Client).Stream` in hub/internal/adapters/anthropic/stream.go; `post` takes an Accept argument; `wireRequest.Stream`
  (omitempty, Complete body unchanged); `stopReason` helper shared with Complete.
- stream_test.go (fake SSE server, -race): normal, split chunks, ping/comments, unknown events, cumulative usage,
  error events, truncation, malformed, oversize line/stream, max_tokens, ctx cancel/deadline, concurrency.
- probe_test.go: runs only with PARATECH_PROBE_ANTHROPIC_URL and _KEY (optional _MODEL). Logs only sanitized event
  names and timings. Not run by agents. Probe result: PENDING the owner's run (docs/qa/tasks/TASK-144.md);
  until then `provider_stream` stays off.

## Decisions to confirm
- Limits: line 1 MiB, whole stream 16 MiB (my choice, not documented).
- Usage missing at message_stop is a permanent "malformed usage" (no charging of an unknown amount).
- `error` event with unknown/unparsable type is permanent "stream error"; known types mapped like HTTP statuses.
- Probe also requires the first delta at least 100 ms before the end (buffering detector); confirm the threshold.
- Non-event-stream content type is a permanent error (gateway ignoring `stream: true` fails loudly).

## Follow-ups
- TASK-145: do not retry Stream after a delta was sent (a transient error event can come mid-stream; duplicates and
  billing); apply the ADR "release" rule knowingly; treat deltas as untrusted (hard rule 3).
- refs section 8 last «our inference» paragraph says the adapter rebuilds the full Message; Stream keeps text and usage
  only. Reword when touching refs.
- Add a test row: `error` event as the very first event (code handles it). Bare CR line ends are not supported (comment).

## Changed existing tests
None.

## Security
Auditor: no high/medium findings (size caps, no provider text in errors, redirects refused, probe skipped without env).
Reviewer: approve, no blockers; golangci-lint 0 issues.
