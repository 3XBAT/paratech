# TASK-140: «Стоп» during a running step (core)

- **Agent:** go-core (opus)
- **Label:** core
- **Depends on:** -
- **PRD sections / AC:** ADR-0021 (budget, journal, Interrupt), ADR-0026 «Waiting» and «Стоп»;
  `paratech-ops/supervisor-decisions.md` (Interrupt keeps real outcomes)
- **Branch:** ralph/<date>-task-140

## Goal
The panel learns the thread and turn of a request before its first Hub step returns, so «Стоп» works at once; after
«Стоп» the Driver runs no further call of the step.

## Allowed to change
- `paratech/desktop/internal/cases/agentturn/{ports.go,types.go,step.go,service.go}` (+ tests, fakes)
- `paratech/desktop/internal/input/agent/{driver.go,tools.go}` and a new `progress.go` (+ tests, fakes)
- `paratech/desktop/internal/input/wails/agentpanel.go` (+ test): `progress` field of the update view
- `paratech/desktop/internal/app/*` only for wiring `Progress`
- `paratech/desktop/frontend/src/bindings/agent.ts`: parse the new `progress` field only

## Out of scope
- Rerun (TASK-141), UI controls (TASK-143), Hub changes.

## Acceptance criteria
- [ ] Optional port `agentturn.Progress { Started(ctx context.Context, st Step) }` in `Deps`; `drive` calls it after
      the open step is saved and before `AgentModel.Step`, with the request state and no cards; nil = no call.
- [ ] `input/agent` implements it over `Sink` (`Update{…, Progress: true}`); `app` wires it with no Driver ↔ Service
      cycle; the update view has `"progress": true|false`.
- [ ] After `Driver.Stop(ref)` the loop of that request starts no further call of the current step; a read (class R)
      in flight is cut through its own context and answered not done; a write (class U) in flight finishes and its
      journaled outcome is kept; then `Interrupt` stores the request `STOPPED` with reason `user` (not `offline`).
- [ ] A Hub step in flight at «Стоп» is not cancelled; «Продолжить» replays its key (existing test kept or added).
- [ ] Tests: progress published before the model call; Stop between two U calls runs only the first; Stop during a
      read gives not done and reason `user`; Stop never cancels a paid job; `-race` clean.
- [ ] `go vet`, `go test -race ./desktop/internal/cases/agentturn/... ./desktop/internal/input/...`, the architecture
      test and `GOOS=windows go vet ./desktop/...` pass.

## Notes
`Started` runs under `Service.mu`: it must only publish. The stopping mark lives in Driver memory per (thread, turn)
and is cleared by the next operation on that thread. ARCH-41: the Driver only stops running calls; the state change is
`thread.Stop`/`Interrupt`.
