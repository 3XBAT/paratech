# TASK-345 — «Сегодня»: тише и понятнее

## Done
Frontend only (no Go, no new dependency). Files: `src/pages/{TodayPage,TodayParts}.tsx`, `src/todayView.ts`, `src/pages/today.css` (the today rules live here, not in `styles.css`), tests.
- No «Обновить». A failed first read: red `role="alert"` «Не удалось загрузить день.» + «Повторить» in place. «Повторить» clears the line, shows an inline «Загрузка…» (`loadView`), moves focus to the section; a repeated failure brings the line back. A failed re-read over a shown day: quiet muted «Не удалось обновить день.» + «Повторить» (no alert).
- «Нужно ваше решение», «Сроки на неделе» (old words added to the `words344` scan). Deadline chip = `dueText(due, now)` (TASK-344 follow-up closed), the weekday/date column is gone, the chip sits under the title with the subject. Urgent chip: clock icon + the dueText word; overdue chip: `fail` + `warn`.
- «Итоги дня»: posts then done rows in one list, a done row already covered by a post is not repeated, hidden when empty; own grid child, last in one column, under «Пары дня» in two columns.
- «Спросить paratech» hidden without chips. Status bar: only queue and nearest deadline (now in the same dueText wording), hidden when both are empty; the «Звук обрабатывается…» line is gone from «Сегодня» (still in `RecordingsPage`, TASK-348).
- `SpeechComponentNotice` under the head of «Сегодня» (not in the `all` mode). Empty blocks: heading + one short line.

## Decisions to confirm
- Day-2 urgent deadline reads «до чт, 5 нояб., 18:00» (clock icon, no relative word) — one wording everywhere; the AC example «сегодня/завтра» holds for those days.
- The page no longer uses `todayErrorText` (specific reasons repeat or are irrelevant); it stays in `bindings/today.ts` (now used only by its test).
- Focus moves to the section on «Повторить» press (the button disappears at once).
- Done rows dedupe against posts of the same recording.
- Deadline-column removal changes the row look (owner to see).

## Follow-ups
- Queue line «В очереди 2 задачи» uses «задачи» (glossary: forbidden) and is not a link to «Записи» (after TASK-348).
- «Загрузка…» shown with no delay (flash on each visit); `role="status"` added together with its text may not be announced; `focus({preventScroll:true})`.
- The lesson still appears twice (lesson row post + «Итоги дня»), finish with TASK-352.
- «Спросить paratech» hint still technical (TASK-355). TASK-348: make the install button of the speech notice secondary on «Сегодня» next to the new primary «Записать пару».
- Tab order in two columns: lessons, right column, then «Итоги дня».
- Glossary additions: «Итоги дня», «Не удалось обновить день.».
- Two tests read source text with regexes (no DOM harness): retry wiring and grid areas.

## Changed existing tests
`today.test.mjs` (deadlineLine fields/chip wording, nearest-deadline wording, urgent/overdue chip kinds, rows regexes, `SummarySection` instead of `PostsSection`/`DoneSection`, empty sections replaced, load-error source checks), `agentconfirm.test.mjs` (calendar button tests use `SummarySection`, assertions unchanged), `screens224.test.mjs` (nearest without `now` -> «до …»), `words344.test.mjs` (four old words added), `homeworkadd`, `today316`, `navhistory` (heading names in titles/comments).

## Review
Reviewer: approve with comments -> fixes -> re-review approve. ux-expert: «доработать» (retry feedback, summary last in one column, chip squeezing the title, status-bar wording) -> fixed; also quiet refresh error and overdue chip. Security-auditor: not run — view-only change, no binding/trust boundary touched.
