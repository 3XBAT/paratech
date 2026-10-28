# TASK-275 — «Вернуть» after the restore window

## Done
- `homeworkView.ts`: `changeTexts.expired` («Задание удалено окончательно, вернуть уже нельзя»), `changeProblemText("expired")`, `ChangeOutcome.expired` (true only for that problem; no assignment, no reload, no «Открыть отправку»).
- `RemovedLine` (`pages/HomeworkDialogs.tsx`): `expired` prop. The same `role="status"` line shows the permanent text, the «Вернуть» button is gone, and the status text (`tabIndex=-1`) takes the focus so it is not lost and the text is read out. «Работа остановлена» is still appended after a stopping delete.
- `HomeworkPage.tsx`: list (`expiredIds`) and detail page (`goneExpired`) set the flag on an expired «Вернуть»; no red refusal line and no reload (it would drop the removed line). A new delete resets the flag. «Удалить» and a successful «Вернуть» are unchanged.
- Tests: `test/homeworkexpiredline.test.mjs` (texts, outcome, both render states, expired + stopped, page wiring, focus effect). Frontend suite 1301 pass, build OK.

## Decisions to confirm
- **The 3-day hint at delete time was NOT added** (the task description mentions it; the acceptance criteria do not). First version had a muted «Вернуть можно в течение 3 дней» next to «Вернуть»; reviewer (medium) and UX expert (must-fix) both found it untrue: the «Вернуть» line lives only while the student stays on the page, and the UI has no list of recently deleted assignments, so the hint promises a return path that does not exist. Removed. The existing TASK-338 stop warning («Вернуть задание можно в течение 3 дней…») has the same weakness. OWNER decision: (a) a «Недавно удалённые» view (reverses C-2 «а» of ADR-0029, needs an ADR) and then the hint with a date there, or (b) reword the TASK-338 warning to what is true.

## Follow-ups (UX expert / reviewer, not done)
- Expired text says what, not why («…прошло больше 3 дней»); the AC fixes the text.
- Drop «· Работа остановлена» in the final state; NVDA may read the changed status text twice (live region change + focus) — check once by hand (focus the `.hw-removed` wrapper instead if so).
- An expired row in the list has no action until the student leaves; optional «Убрать из списка».
- Two undo patterns (inline «Вернуть» vs the done toast), the line does not name the assignment — in the UX audit (TASK-316 follow-ups).
- Wiring tests are source-pattern regexes (no DOM harness in the repo).

## Changed existing tests
None.

## Security
Auditor (opus): no findings (fixed strings as text nodes, no new calls or side effects; the server still decides). Reviewer: approve, medium on the hint (fixed by removal). UX expert: «доработать» on the hint (fixed by removal); the rest are suggestions above.
