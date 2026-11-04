# TASK-269 — compaction before Ask in the Driver, `compacting` in agent:update

## Done
- Key (supervisor 04.11): `SummaryRequest.Key()` = `cs:<thread>:<through>:<hash8>`, hash8 = first 8 hex of SHA-256 of the exact JSON data sent (one helper `summaryData` shared with `SummaryEncodedSize`). A refused range is retried once its content changes; a «Скрыть» restart no longer meets IDEMPOTENCY_CONFLICT. Hub accepts it (1-128 printable ASCII, key about 46 bytes). ADR-0028 line, hubclient doc, paratech/README.md and a comment in hub/tests/chatsummary_eval_test.go updated.
- `agentcompact.Compact(ctx, id, started func())`: `started` is called once, right before the Hub call (not for no-compaction, busy, refused-before, early failures). `Result.Thread` added. `fail` logs only chat id and error code (never the cause). One lock per chat instead of one global mutex (review medium: an Ask in chat B waited for chat A's Hub call); ref-counted, context-aware wait; `refused` has its own mutex.
- `input/agent`: `Compactor` port and `compact.go`; `Ask` compacts inside its goroutine before `turns.Ask` (named chat only). Updates: `{Compaction, Compacting:true}` at `started`, then `{Compaction, Compacting:false, Fill}` (Fill nil on failure or panic). Failure or panic never touches the request: logged by short code only, ErrBusy not logged by the Driver.
- `input/wails`: `AgentUpdate`/`AgentUpdateView` get `compaction`, `compacting`; `fill` from the update when set; compaction updates skip the fill read and `draftsBefore`. Tests: no chat binding or update returns the summary text (real agentchats service with a marker, plus a reflection guard).
- Frontend (bindings/agent.ts, AgentPanel.tsx, agentState.ts): `compaction`/`compacting` parsed; a compaction update only moves the ring (fill), never reaches the request reducer. Nothing visible changes.

## Decisions to confirm
1. Wire shape: a compaction update is marked by `compaction:true` (turn 0, status "", no cards); `compacting` says running/ended. Without the marker such an update looks like a failed Ask to the page.
2. The pair of updates is published only when a Hub call is actually made (via `started`), not around every `Compact`: no banner flash for the ~9 of 10 requests that need no compaction. Differs from the literal AC wording.
3. An Ask with an empty ThreadID (current chat) is not compacted: the Driver cannot name the chat before the Hub call. The page sends the open chat's id, so this is only the no chat open case.
4. Edit outside the file list: `domain/agent/thread/compaction_test.go` (exact key -> prefix check `cs:th1:%d:`); `hub/tests/chatsummary_eval_test.go` (comment only).

## Follow-ups
- TASK-270: render the banner from `compaction`+`compacting` (add page state per thread); ring overflow can show without any compaction after an expired-request restart (`historyFrom` is mirrored from `NextFill`, so requests before it are never summarized); no banner for an Ask without ThreadID.
- A chat stuck in PLANNING/EXECUTING with no live loop: the first Ask is unstuck and retried without compaction (accepted).
- Some failures are still logged twice (agentcompact `fail` and the Driver line for codes other than busy): drop the Driver line later.
- HistoryReadLimit vs very long never-compacted chats (from 268, unchanged).
- Low: the new "other chats do not wait" test hangs (not fails fast) against a context-ignoring global mutex.

## Changed existing tests
agentcompact/service_test.go and app/agentcompact_test.go (new `started` argument, `Result.Thread`, key prefix checks); wails agent_test.go, agentcurrent_test.go, agentfill_test.go (pinned event JSON gains `"compaction":false,"compacting":false`); values/summaryrequest_test.go and hubclient/summary_test.go (key with hash); thread/compaction_test.go (prefix). No test weakened or skipped.

## Security
Auditor: no high or medium. Texts: nothing but chat id and codes in logs; panic value never logged; the summary never reaches an update or binding. 8-hex hash collision needs the same tester, chat and Through with about 2^16 content versions and would at worst replay an older summary of the same chat: low/informational. Reviewer: approve, medium (global mutex) fixed and re-reviewed, approve.
