# TASK-248 — «Начать заново отсюда» → «Скрыть» (core + binding)

## Done
- Domain `thread.RestartFrom(n, request, now)` (`restart.go`): only when the current request is final (a running one is `ErrTurnInProgress` + `ErrInvalidTransition`, one waiting for the student `ErrInvalidTransition`), `1 ≤ n ≤ turns`. It starts the new request as `Ask` does and records `DropsFrom = n` on it; the summary is cleared when `n ≤ summary.Through`, kept otherwise. `restoreTurn` rejects a `DropsFrom` that is not earlier than the request.
- Store: `AgentThreadRepository.Save` marks requests `n…new-1` with `dropped_by = new number` in the same transaction as the new request and the version CAS (`DropAgentTurns`); `FirstDroppedAgentTurn` rebuilds `DropsFrom` on load. A second restart re-marks already hidden requests with the new number. Nothing is deleted; actions, journal and reply lines are untouched. No migration (`dropped_by` is from 0040).
- Dropped rows are excluded in SQL before the history read limit (`ListAgentSteps`); `Window`, `Fill`, `CompactionPlan` and `agentchats.Earlier` skip them too.
- `thread.NextFill` honours `HistoryFrom` (a request the agent no longer sees and no summary covers counts as Overflow; dropped ones never do). `agentchats.fillOf` uses it, reading from request 1 with `thread.HistoryReadLimit`; `agentturn` now uses `HistoryReadLimit` too (supervisor decision of TASK-251).
- `agentturn.AskInput.RestartFrom` (needs a chat id; `< 0` or no chat id is `ErrInvalidInput`); Wails `AgentAskInput.restartFrom` (invalid → `invalid`); `bindings/agent.ts` field. No UI (TASK-249).
- Tests: domain (transitions, refusals, summary clear/keep at Through, re-restart, restore checks, Window and Fill agree, NextFill), use case (incl. rollback), sqlite (marks, atomic rollback, re-restart, idempotent re-save), agentchats (Earlier without dropped, ring drops after a restart), wails (JSON shape, validation), ts binding.

## Decisions to confirm
- `RestartFrom` leaves `HistoryFrom` unchanged. If an expired-request restart earlier moved `HistoryFrom` to 5 and the student restarts from 3, the agent still does not see requests 1–2 that the chat shows (ring shows full). Alternative: `historyFrom = min(historyFrom, n)`. Decide before TASK-249 (reviewer, low).
- The sqlc query files are outside the literal `agent*.go` pattern of the task; they hold only the `dropped_by` queries.

## Follow-ups
- TASK-249 (UI) offers the action only on requests that `Earlier` returns (the domain accepts `n` of an already hidden request; harmless).
- `CompactionPlan` still counts `Fill` without the `HistoryFrom` cut; when TASK-268/269 wire compaction its trigger must agree with `NextFill`, else Overflow after an expired restart never clears.
- Restore does a third read (`FirstDroppedAgentTurn`) outside a read transaction, same as `GetAgentTurn`; the CAS rejects a stale save.

## Changed existing tests
- agentturn `fakes_test.go` (History fake copies the SQL: dropped rows left out before the limit; thread fake applies marks on Save); agentchats `fill_test.go` (Fill now via `NextFill`, same expectations plus restart cases).

## Review
Reviewer: approve, 3 lows (above). Security: no findings.
