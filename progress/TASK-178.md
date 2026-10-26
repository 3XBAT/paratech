# TASK-178: «Остановить» writing, active/jobState, EditOutlineAt (core)

## Done
- `turnkey.StopWriting` over a `JobCanceller` port (app: existing runnerCanceller over jobrunner.Cancel). No job: unchanged. Sections stay, Regenerate works.
- `TurnkeyWriting` gets `active` and `jobState` on every read (case builds every view via `s.view`).
- `TurnkeyService.StopWriting(id)` and `EditOutlineAt(id, version, items)`; stale version or lost save -> ErrChanged, text «Работа изменилась: обновите карточку.»; no-op edit does not bump the version (`work.SameOutline`). EditOutline unchanged.
- `bindings/turnkey.ts`: stopWriting, editOutlineAt, active, jobState (+ tests).
- Binding is extendable (TASK-179/185/187): add to `TurnkeyUseCase` plus a thin wrapper using reply/started/fail.

## Decisions to confirm
1. Canceller port is optional: missing -> ErrNotConfigured (desktop/tests build WritingDeps without it).
2. Stale plan reuses `ErrChanged` (Undo's error), not a new ErrTurnkeyStale.
3. `job` field now = the job the call started, else the active one ("" when none; a reused completed job is not shown).
4. Stop of a RUNNING step is asynchronous: the returned view stays active/running until the step returns; the UI must keep polling. If cancel is refused and the job is still RUNNING (crash leftover): new `ErrNotStopped` «Не удалось остановить написание. Попробуйте ещё раз.».
5. A failed job lookup in `view()` is logged and the job is omitted (UI cannot tell it from "no job").
6. Each stop+continue uses one of 10 writing key rounds; after 10 in one generation `ErrWritingRetries` until Regenerate/EditOutline. Not changed.

## Follow-ups
- UI task: «Остановить» button, polling until active=false, switch to editOutlineAt.

## Changed existing tests
- cases/turnkey `writing_fakes_test.go` (fake canceller wired; models real runner), `input/wails/turnkey_test.go` (JSON now has active/jobState; fake methods), frontend `turnkey.test.mjs` (fake + method-name test).

## Security
Auditor: no high/medium findings (job found by work id prefix, no cross-work cancel; version check atomic via optimistic save). Reviewer: approve; two mediums fixed.
