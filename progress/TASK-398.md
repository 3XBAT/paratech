# TASK-398: a second question always queues (Withdraw, FIFO ≤5), and a cut stream after the first Hub message stops `failed`

## Done
- `cases/agentturn/queue.go` (new): in-memory FIFO per chat, own mutex (never `s.mu`, so `Ask` does not wait for a running Hub step), `MaxQueued = 5` (the 6th: `thread.ErrTurnInProgress`), tickets `q<N>` from one counter, a ring of 16 started tickets per chat. `Enqueue` queues when the chat's request is PLANNING/EXECUTING (Hub step or tool run) or something already waits; `Ask` calls it first and returns `Step{Queued, Ticket}` at once; if the aggregate refuses under `s.mu` (`ErrTurnInProgress`), the question is queued then. `StartQueued` (under `s.mu`) pops the oldest when the chat has no request or a final one (DONE, CANCELLED, STOPPED for any reason) and runs the Ask path with the question's own scope, text, uploads, mentions (deleted ones dropped) and RestartFrom; `Withdraw` -> withdrawn | started | not_found; `HasQueued`, `QueueReady` (read-only probe). The ticket rides on every Step/progress of the first drive of a queued-started request.
- `input/agent`: `Driver.Ask` returns `(agentturn.Queued, error)`; it enqueues synchronously. Every loop drains its chat after `forget` (last loop to end wins; a per-thread `deferred` mark and a `later` list cover loops whose thread is not named yet); drain = compaction (only when `QueueReady`) + `StartQueued`; a running request with no loop is recovered by `unstick`. Close sets `closed` first, so nothing waiting is ever sent. `Update.Ticket`, `Update.Queued`; `Driver.Withdraw`.
- `input/wails`: `AgentService.Ask` -> `{problem, queued, ticket}`; new `AgentService.Withdraw(threadId, ticket)` -> `{result}` (withdrawn | started | not_found | invalid | failed); `agent:update` always carries `ticket` and `queued`. `app/agent.go`: `panelUpdate` copies them.
- Frontend (`bindings/agent.ts`): `AskResult`, `parseAskResult`, `WithdrawResult`, `withdraw`, `UpdateView.ticket/queued`; the one caller in `AgentPanel.tsx` adapted (a queued question counts as started until TASK-399).
- `adapters/hubclient/agent.go` (D4): a streamed agent step that ends without a result, or is cut by the watchdog (silent / over the cap), AFTER the first Hub message carries `values.ErrHubFailure` too (still transient, so the automatic retries run first), so the request stops `failed`, not `offline`. Before any message nothing changes.

## Decisions to confirm
1. A question is queued also when the chat's request waits for the student (AWAITING_*) and something already waits, to keep FIFO; the queue then waits until the card is answered or cancelled (the student can withdraw). With an empty queue AWAITING_* is unchanged (Ask cancels the card).
2. A queued question starts after ANY final stop reason, offline and quota included (as the task says); with the Hub down the waiting questions fail one after another.
3. A transport reset after the first message (no watchdog cut) is still `offline`: only "ended without result" and the watchdog count as the Hub's failure.
4. A drain that cannot read the store ends idle without retry; the question waits until another loop of that chat ends or the student withdraws it.
5. A queued "Скрыть" (RestartFrom) runs when it starts, so it also hides the request that ran meanwhile.
6. Tickets are not stable across restarts (queue is not persisted, by design).

## Follow-ups
- TASK-399 must: show the waiting row with the icon «Отменить этот запрос» (Withdraw), replace the row by the update that carries `ticket`, and IGNORE updates with `queued: true` and turn 0 (late-queued race: `Driver.Ask` returned `queued:false`, the ticket arrives as an update; `frontend/src/agentState.ts` ~326 would otherwise treat it like a failed Ask); switch the offline card to `retry` (TASK-396 decision 2).
- Low (review): no test for "idle drain running, an Ask for the same chat defers to it, it must drain again"; `QueueReady` comment should say "for an existing chat"; `TestFrontendCallsBoundNames` does not cover `AgentService` (agent.ts builds names from a template) — a Go test matching every exported AgentService method in agent.ts would close it.
- ADR-0026 addendum point 7 could mention decisions 1 and 4.
- Real E2E (agent pack) not run: ADR-0035 pack directory does not exist yet.

## Changed existing tests
- agentturn `service_test.go` `TestSubmitChecks` ("Ask while calls run" now queued, not `ErrTurnInProgress`), `restartfrom_test.go` ("running calls": queued, nothing changes) — the owner's decision.
- Signature changes only: `Driver.Ask` now returns two values (input/agent tests, `desktop/tests` agent/turnkey/answering files); fakes `scriptedTurns`, `stopTurns`, `stuckTurns`, `jobTurns` got the queue methods; wails fake driver; exact-JSON tests of `agent:update` gained `"ticket":"","queued":false`.
- Frontend: three assertions of `api.ask` that expected a bare string now expect `{problem, queued:false, ticket:""}`.

## Security
security-auditor (opus): no High/Medium; clean on ticket/thread validation at the Wails boundary, withdraw isolation, double start, start after Close/Withdraw/chat deletion, per-chat cap, hubclient change (ErrHubFailure does not change retry class, key or billing; quota still first). reviewer (opus): 1 high (two idle drain loops re-waking each other), 3 medium (AWAITING ambiguity -> decision 1, compaction before readiness, cut by transport -> decision 3), lows; high and mediums fixed in aa66a616 and re-reviewed: approve.
