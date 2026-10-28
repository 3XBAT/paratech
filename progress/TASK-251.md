# TASK-251 — context ring data (ADR-0028)

## Done
- Domain `thread/fill.go`: `ContextFill.Percent()` (integer floor of max(pairs/20, bytes/48 KiB, turns/64), cap 100; Overflow = 100) and `Level()` (the one level function: <70 normal, 70-89 high, >=90 full; `FillLevel` strings go to the UI). `HistoryReadLimit` = the step's history read limit.
- `cases/agentchats`: `Fill{Percent, Level, Overflow}`, `Chat.Fill` (nil when the history read failed), `Service.Fill(id)`; List/New/Rename/Archive/Restore return it. The next request is `turn+1`; history is read with `Since(id, th.HistoryFrom(), HistoryReadLimit)`, so the TASK-203 follow-up «Fill ignores HistoryFrom after Restart» is fixed. Uses `th.Summary()`: a stored summary (TASK-266/269) lowers the fill (tested with a summary).
- `input/wails`: `fill` `{percent, level, overflow}` or null in `AgentChatView` and `AgentUpdateView`. `AgentPanel.SetFills` (wired in `app/agent.go`); the update carries it only at the end of a request (DONE, STOPPED, FAILED, CANCELLED, not Progress, thread known), never during a step.
- Frontend `bindings/agent.ts`: strict `parseFill` (null for anything malformed); no rendering (TASK-208), no thresholds in TS.
- `input/agent` Driver untouched (the panel computes the fill); no migration.

## Decisions to confirm
1. STOPPED counts as the end of a request (a «Продолжить» then sends that request as the current one, so the ring may overstate by its own pair until the next end).
2. Overflow shows as 100 % / full (ADR: «shown as full»).
3. An empty chat is 0 % without a history read; `fill` is null when a chat's history cannot be read (the list still comes).
4. Files outside the list: `domain/agent/thread/fill.go` (+test) and one wiring line in `app/agent.go`.

## Follow-ups
- `agentturn.historyLimit` duplicates `thread.HistoryReadLimit`; switch agentturn to the domain const once TASK-266 is merged (not touched here to avoid a conflict), and add a test that they agree.
- `List` reads the history of every chat, archived included (up to ~460 rows each); if the list grows, skip archived chats (their fill comes with Restore) or cache.
- The fill read runs on the Driver goroutine inside Publish (2 s timeout, local DB); if it ever shows, compute it in the use case that ends the request.
- A failed fill read in List is not logged (the service has no logger); log it in `input/wails` if a nil fill shows up.
- TASK-269 sets the fill after a compaction (the update that carries `compacting`).

## Changed existing tests
`input/wails` agent_test / agentcurrent_test / agentchats_test: exact-JSON expectations gained `"fill"`; `frontend/test/agentchats.test.mjs`: chat fixture gets `fill: null`.

## Security
Auditor: no findings (only ints, an enum and a bool leave; ids validated; no error text to the UI). Reviewer: approve; medium items (duplicate limit, List cost, DB read in Publish) are recorded above, comments added for the last.
