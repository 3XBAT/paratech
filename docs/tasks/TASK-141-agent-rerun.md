# TASK-141: «Повторить» a finished request (core)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** TASK-140
- **PRD sections / AC:** ADR-0021 (threads, journal, Undo), ADR-0026 «Повторить»; hard rule 3, 7
- **Branch:** ralph/<date>-task-141

## Goal
The student can run the last finished or stopped request of a thread again, never doubling a write or a proposal.

## Allowed to change
- `paratech/desktop/internal/domain/agent/thread/*` (`Rerun`, `ErrRerunBlocked`) (+ tests)
- `paratech/desktop/internal/cases/agentturn/*` (`Rerun`, port `Effects`) (+ tests)
- `paratech/desktop/internal/cases/agentjournal/*` (method implementing `Effects`) (+ tests)
- `paratech/desktop/internal/adapters/sqlite/*` only if the journal needs a query by (thread, turn)
- `paratech/desktop/internal/input/agent/driver.go` (+ tests), `paratech/desktop/internal/input/wails/agent.go` (+ tests)
- `paratech/desktop/internal/app/*` (wiring), `paratech/desktop/frontend/src/bindings/agent.ts` (`rerun`, problem)

## Out of scope
- The answer key scope (TASK-142), UI (TASK-143).

## Acceptance criteria
- [ ] `thread.Rerun(live bool, now)`: only on the current request in `DONE`, `FAILED`, `CANCELLED` or `STOPPED`; else
      `ErrInvalidTransition`; `live` → `ErrRerunBlocked`, nothing changes. A stopped request is cancelled and its open
      calls closed not done; a new request (next turn, empty budget) starts in the same scope.
- [ ] `agentturn.Rerun(ctx, ref)` reads `Effects.Live(ctx, thread, turn)` (true when the request has a journal entry
      not `FAILED`/`UNDONE`, or an open proposal), adds the stored student text again and drives the new request.
- [ ] `Driver.Rerun`, `AgentService.Rerun(ref)`; problem `rerun_blocked` in the Driver enum and the Wails allow-list.
- [ ] New step keys `ag:<thread>:<turn+1>:…`; a second Rerun on the old turn is `stale`; a running request is `busy`.
- [ ] `input/agent` interfaces still expose no `Confirm/Execute/Delete/Send` (architecture test).
- [ ] Tests: each status allowed/refused; live effects refused (DONE action, open proposal), allowed after UNDONE;
      double click; rerun from STOPPED offline with open calls; text and scope reused.
- [ ] `go vet`, `go test -race` of the touched packages, the architecture test, `GOOS=windows go vet ./desktop/...`.

## Notes
`agentturn` must not import `agentjournal` (ARCH-20): the port is declared in `agentturn` and `app` plugs in the
journal use case (the `answering.Knowledge` pattern).
