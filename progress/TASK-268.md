# TASK-268 — desktop context compaction (agentcompact, Summarize in hubclient, summary turn)

## Done
- Domain (new files, outside the task list: an adapter may not import a use case, so the port's request is a domain value): `values/summaryrequest.go` (`SummaryPair`, `SummaryRequest`, `NewSummaryRequest`, Hub limits as named constants, `Key()` = `cs:<thread>:<through>`, `EncodedSize`, `Equal`) and `thread/compaction.go` (`Thread.CompactionRequest(entries)`: plan via `CompactionPlan(entries, turns+1, summary)`, builds the pairs, shortens to the Hub limits).
- Use case `cases/agentcompact`: `Compact(ctx, id)` (empty id = current chat) -> `Result{Compacted, Fill}`. Load, read history, plan, ONE Hub call (global mutex), `ParseHistorySummary`, `thread.Compact`, save. Codes: `ErrInvalidInput`, `ErrInvalidConfig`, `ErrBusy`, `ErrChanged`, `ErrQuota`, `ErrUnavailable`, `ErrRefused`, `ErrStore`; every failure matches exactly one; nothing is saved on any of them.
- Version conflict: no second Hub call; the chat and its history are reloaded, the request is rebuilt, and the received summary is applied only if the rebuilt request `Equal`s the one sent (so a «Скрыть» restart made during the call is never undone); at most 3 saves, else `ErrChanged`.
- hubclient `Summarize`: `Complete` with `OPERATION_TYPE_CHAT_SUMMARY`, unary, bounded by `c.timeout` only, `MaxOutputTokens` 0 (Hub cap 4096), strict token-based answer parsing (exactly `{"summary":"<string>"}`, repeated or extra key, trailing data refused). `agentRequestAt(req, catalog)` drops the summary turn below catalog 9 (CatalogVersion stays 9).
- app: `wireAgent` builds `c.compact` (wiring only, nothing calls it until TASK-269). `testenv/agenthub.go` (outside the list) accepts a leading `history_summary` on catalog >= 9.

## Decisions to confirm
1. Shortening is a domain rule: each part cut on a rune boundary (student 4 KiB, reply 2 KiB, beginning kept); if the JSON (no HTML escaping, same encoder in domain and adapter) is over 64 KiB the longest part is cut repeatedly; pairs with a blank student text are skipped; none left = no compaction.
2. `historyFrom` mirrored from `NextFill`: requests before it are not summarized and trigger nothing; the ring can show overflow after an expired-request restart while no compaction runs (info for TASK-270).
3. Refused keys (Hub refusal or text failing `ParseHistorySummary`) are remembered in memory (256, oldest dropped) and not sent again; other permanent errors (bad key) and network errors are not remembered.
4. A running request or an open step is `ErrBusy` before any Hub call.
5. One global mutex held during the Hub call: a slow compaction of chat A delays the compaction (not the Hub step) of chat B up to the request timeout; TASK-269 should keep it before `Ask`.

## Follow-ups (owner / next tasks)
- **Refused range is stuck**: the key `cs:<thread>:<through>` is fixed for a chat until something is summarized, and the Hub replays a charged refusal for it forever; ADR «the next Ask tries again» cannot hold. Needs a decision: an attempt suffix in the key after a refusal, or a Hub that does not replay refusals for this operation.
- **Key reuse with other content**: after «Скрыть» clears a summary, a later compaction with the same `through` but different pairs gets IDEMPOTENCY_CONFLICT from the Hub (permanent, `ErrRefused`). A content hash in the key would fix it (same decision as above).
- The history read is the last `HistoryReadLimit` entries: in a long never-compacted chat the oldest unsummarized requests may lie outside the read, and the summary then claims to cover 1…through without them (the agent does not see them today either). Guard or log later.
- Low: `summaryOf` accepts an escaped key (`"summary"`), the same JSON value; the `once := true` local in one subtest shadows the `once` helper; a comment on `Equal` that the previous summary's Through is not compared would help.
- TASK-269 must log the code only (never texts) and publish the banner; TASK-270 shows it.

## Changed existing tests
None changed in meaning. In new `agentcompact` tests the "another summary meanwhile" case expects `errRequestChanged` (written during the fix round). `testenv`: new `agenthub_test.go` only.

## Security
Auditor: 0 high, 0 medium. One below-threshold note (a stale re-apply after «Скрыть» during the Hub call) was independently found by the reviewer as medium-high and fixed (rebuild + `Equal`, tested to fail without the fix). Summary is data only: parsed by `ParseHistorySummary`, never in `seen`, never shown, never in logs or errors. Reviewer: approve after the re-review.
