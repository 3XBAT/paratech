# TASK-224 — design follow-ups of TASK-202 (markup only)

## Done (frontend only, existing data and bindings)
- «Журнал»: groups «Сейчас» (in-progress statuses) / «Сегодня» / by date / «Без даты»; time-only lines inside dated groups. Item 14.
- «Настройки»: shared head + left menu (wraps below 900px container width); section heads demoted to h2, one h1 on the page. Items 17, 18.
- «Задания»: filter «Все / Активные / Отправленные» with counters; split filters disabled until deliveries are loaded or when every load failed. Item 10.
- «Сегодня»: status line (queue of recordings, nearest deadline, local-processing note). Item 12.
- Tests: 9+4 in test/screens224.test.mjs (812 pass), build ok.

## Decisions to confirm
- «Отправленные» instead of the mockup's «Сданные» (the app knows only that an email was sent).
- No «Ждут «Да»» filter: no per-assignment confirmation data in the list.
- PROPOSED (waiting for «Да») sits in a date group; only running states are in «Сейчас».
- The «Сегодня» queue count covers recordings only.

## Follow-ups
- Go: subject page (item 7) needs per-subject nearest assignments with due dates, teacher, schedule summary, lessons with status, «new» flag. Not built.
- Go: per-assignment confirmation state for a «Ждут «Да»» filter; queue count of all jobs for «Сегодня».
- Not done: «Задания» day/date column, «Журнал» time column.

## Changed existing tests
- «the journal renders a heading with a count per group»: accepts the new id attribute on the group heading.

## Review
Reviewer found 1 layout regression (settings wrappers on all pages), double h1 and minor issues; all fixed in the second commit. Not re-reviewed.

## Security
Auditor: no findings (UI only, no HTML injection, no new calls or deps).
