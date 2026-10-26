# TASK-186 — assignment page: «Быстрый черновик» + «Сделать под ключ»

## Done
- HomeworkPage: «Решение» → «Быстрый черновик»; «Новый черновик»/«Доработать черновик»; chips «Черновик не готов/готов»; also «Готовим черновик…», «Черновика ещё нет», send dialog `<dt>Черновик</dt>`.
- New block «Сделать под ключ» (`pages/HomeworkTurnkey.tsx`, pure logic in `homeworkTurnkey.ts`): loads `workOfAssignment` on open; a work exists → panel's `WorkWritingCard` unchanged; none → caption and button; click → `startForAssignment`, button disabled while pending, double click = one call (`singleFlight`); errors via `turnkeyErrorText`. No «платно» marks.
- WorkCard.tsx untouched (TASK-191 owns its buttons).

## Decisions to confirm
- Placement: separate card after «Новый/Доработать черновик», before «Версии».
- Extra files outside the task list: `homeworkTurnkey.ts`, `pages/HomeworkTurnkey.tsx`, `tsconfig.test.json` entry.

## Follow-ups
- Medium (not fixed, same limitation in the panel): when triage fails offline/quota/refused/no_text the core returns the work with `problem/message`, the page keeps only `w.id`, so the card shows «Задание ещё не разобрано» + «Повторить» instead of the reason. Fix: show `w.message` above the card.
- Texts still saying «решение» under the draft block: `bindings/homework.ts:179-186` and Go `input/wails/homework.go:30-36` (ErrDraft*) — small Go+binding task.
- TASK-102 guard test exemption is whole-file for 3 files; could be narrowed to the approved literals.

## Changed existing tests
- `test/homework.test.mjs`: chip/generating labels now «Черновик…»; the TASK-102 "no «черновик» in UI" guard exempts HomeworkPage/HomeworkDialogs/homeworkView (owner decision 26.10).

## Security
Auditor: no findings (no HTML sinks, click is consent, backend idempotent, errors from a fixed list).
