# TASK-272 — snapshot of a chat's current request (ADR-0028)

## Done
- `agentturn.Service.Current(ctx, id)`: read-only (no Hub, no save, no lock); plan and questions cards rebuilt by the shared `planCard`/`questionsCard` helpers; stopped card via `stepOf`; running = status only.
- `input/agent.Driver.Current` reuses `update` (the card checks of `publish`), publishes nothing.
- `AgentService.ChatCurrent(threadId)` returns `{"problem": "", "update": <agent:update JSON> | null}`; the update is built by `AgentPanel.view` (byte-equal to the event, tested). input/wails does not import input/agent; mapping in `app/agent.go` (`driverCurrent`).
- null when the chat has no request or it is DONE/FAILED/CANCELLED. STOPPED returns «Продолжить». Missing chat: `not_found`; bad id: `invalid`.
- `bindings/agent.ts` `chatCurrent` (same `parseUpdate` as the event; problem as chatEarlier). Not wired into AgentPanel (TASK-273).
- Tests: agentturn/current_test.go, input/agent/current_test.go, input/wails/agentcurrent_test.go, app/agent_test.go (real sqlite), test/agentchatcurrent.test.mjs.

## Decisions to confirm
- Wrapper `{problem, update}` instead of a bare update/null: the update problem set has no `not_found`.
- A pending propose_* confirm card of an ended (DONE) request gives null (the task says final -> null); it stays reachable on «Сегодня». Only a plan waiting for «Да» (AWAITING_PLAN) and questions show live. TASK-273 owner step must say «Да» of the plan. Restoring confirm cards in final requests = separate follow-up + ADR-0028 note.

## Follow-ups (lows)
- `Current` reads thread and history non-atomically; a race gives `failed`, retry heals (reviewer).
- `unavailable` is mapped to `failed` in TS; a future waiting status would show no card silently.

## Existing tests changed
None (two input/agent fakes gained a `Current` method; no assertions changed).

## Security
Auditor: no findings. Reviewer: approve.
