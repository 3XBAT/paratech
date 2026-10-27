# TASK-206 — agent chats binding

## Done
- `input/wails.AgentService`: RenameChat, ArchiveChat, RestoreChat, ChatEarlier and the chat list; Ask takes `threadId` and `mentions`; `agent:update` carries `threadId`; journal view carries `chatTitle`. Chat calls return enum problems (`invalid`, `not_found`, `busy`, `failed`), never error text; ErrBusy/ErrInvalidTransition map to `busy`.
- `app`: `agentchats` wired; `AgentCore.complete()` now requires Chats.
- `input/agent/driver.go`: `unstick` gets `IdleInput{Thread: in.ThreadID}`.
- Frontend `bindings/agent.ts` (parsers only; the panel is TASK-207).

## Files outside the task list (supervisor-widened scope)
- `adapters/sqlite/agent.go` + test: `RequestMentions` next to `RequestScope` (strict JSON, bad rows refused).
- `cases/agentturn/service.go`, `step.go`, `redo_test.go`: no lost student message when a chat change races Ask. Not a shared lock: the first save of an Ask that hits a version conflict is rebuilt once on the stored chat (before any Hub call, so no double request).
- `cases/agentchats/service.go` (TASK-205 code), `cases/agentjournal`: no-op change is not saved; journal reads chat title.

## Review fixes (reviewer + auditor)
- No-op Restore/Archive/Rename no longer bumps the version (it dropped a running step's paid reply).
- Card answers («Да», «Изменить», typed answers) get the same one-shot redo after a Rename.
- Comments: Ask into a missing chat is an update with problem `failed` (the update problem set has no `not_found`); MaxMentions is 8.

## Decisions to confirm
- Ask into a missing chat reports `failed`, not `not_found` (adding a code widens the driver/wails/frontend contract).
- Retry instead of a shared per-thread lock.
- ADR-0028 Open 1-7: nothing decided. `ChatEarlier` returns the stored mention-time label; how it is shown is TASK-247.

## Follow-ups (lows)
- No test for Stop winning over a card answer; RevisePlan not in the redo test.
- Moves could report `changed bool` so the domain owns the no-op decision.
- A real rename during a running step (after its first save) still conflicts the step's later save; RecoverIdle/«Продолжить» recovers.
- Two empty chats possible on simultaneous Ask-without-chat and «Новый чат» (separate mutexes); harmless.
- Journal loads the whole chat per thread for its title; a narrow `Title(id)` read later.
- TASK-207 must render `title`/`chatTitle` as plain text.

## Changed existing tests
`frontend/test/agent.test.mjs`, `input/wails` agent/agentjournal/shell tests, `agentjournal` service test: adapted to the new fields/signatures (threadId, chatTitle, Threads.Get).

## Security
Auditor: no High/Medium. Ids validated, mentions bounded and existence-checked, no error text to UI. Reviewer re-review of the fixes: approve.
