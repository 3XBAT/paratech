# TASK-191 — work card buttons (UI)

## Done
Files: `desktop/frontend/src/agent/WorkCard.tsx`, `src/workView.ts`, `test/work.test.mjs`.
- «Остановить» while `writing.active` -> `stopWork` (stopWriting). One poller keeps reading while the job is active; the button reads «Остановка…» until it is gone. Refusal: «Не удалось остановить написание. Попробуйте ещё раз.» (no «Повторить»).
- Stopped state (step is a writing step, `active=false`): «Написание остановлено» + «Продолжить» (= startWriting). Polled slowly (retryMs) so a job alive after a transient core read error reappears.
- Plan edit via `editOutlineAt(id, version, items)` for approve/rewrite/save; stale -> «Работа изменилась: обновите карточку.» + re-read.
- «Открыть в Word» / «Открыть в PowerPoint» via `libraryApi.openMaterial(materialId)` (id only) for a ready work, with a caption that a copy opens and edits do not return. Optional `library` prop of `WorkWritingCard` (default from `callByName`); props otherwise unchanged for TASK-186.
- Every click goes through `run`/`singleFlight`.

## Decisions to confirm
- Wording «Написание остановлено» / «Продолжить» (not specified by the task). It also shows for a job that failed for good (core doc: "stopped or ended early").
- `workView.ts` touched (pure logic by convention), slightly beyond "card + tests".

## Follow-ups
- The PowerPoint button is unreachable until TASK-106 gives a presentation a writing result; the lab report and papers use Word.
- A core older than TASK-178 (no `active`) would show the stopped state; frontend and core ship together, so ignored.
- Button clicks are tested through logic functions and static markup (no DOM in the test setup).

## Changed existing tests
`approvePlan`/`rewriteWork`/failing-save tests: pass a version, expect `editOutlineAt`. «buttons without a binding are not offered» no longer bans «Открыть в Word»/«Остановить» (bindings exist); «Отправить преподавателю» still banned.

## Review
Reviewer: HIGH (dead end after stop), MEDIUM (untested stop handler, unbounded second poller), LOWs fixed. Re-review found HIGH («Остановка…» stuck after «Продолжить») and MEDIUM (transient read error ends polling): fixed; that last fix commit was checked by the coordinator's own diff read, not a third reviewer run.

## Security
Auditor: no findings (only materialId passed; model text and file names as text nodes; fixed error texts; single-flight; polling cleaned on unmount).
