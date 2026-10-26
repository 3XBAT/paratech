# TASK-185: «Сделать под ключ» со страницы задания (core binding)

## Done
- `TurnkeyUseCase.Triage`; `TurnkeyService.StartForAssignment(assignmentId)` runs the same `turnkey.Service.Triage` as the agent's triage_assignment with `work.Source{Assignment}`. Result mapped by `reply` like Retry (offline/quota/refused/no_text/source_gone keep the work with a problem). No agent journal entry; the click is the consent. Nothing changed in cases/turnkey or app (shell already passes `*turnkey.Service`).
- `bindings/turnkey.ts`: `startForAssignment` (+ tests).
- Tests: new triage, repeat click = one reader call, offline then retry-by-click, unknown/empty assignment, error mapping.

## Decisions to confirm
1. A work still in `triage` (earlier call failed) is asked again on click, same idempotency key (Hub replays free if answered). Matches "errors as Retry"; an existing work in any later status costs nothing.
2. New text `ErrTurnkeyNoSource` «Задание не найдено: возможно, его удалили.» (the existing not-found says «Работа…»). Used when no work was saved; if a work was saved and the assignment vanished, the reply is the work with problem `source_gone`.
3. A concurrent double click can reach the reader twice with the same key; relies on Hub idempotency. UI (TASK-186) should disable the button while pending.

## Follow-ups
- TASK-186: the button. Possible orphan local work row when the assignment id is unknown but the insert succeeds (auditor note; real SQLite hits the FK and returns no work).

## Changed existing tests
- `input/wails/turnkey_test.go`: fakeTurnkey gets a `Triage` method; frontend `turnkey.test.mjs` fakeApi gets `startForAssignment`.

## Security
Auditor: no findings. Reviewer: approve, lows only.
