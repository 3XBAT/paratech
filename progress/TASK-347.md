# TASK-347 — «Записи», «Предметы» и диалоги: тише, кнопки по-Windows

## Done
Frontend only (no Go, no new dependency), pure view state.
- Empty «Записи»: block `.rec-empty` with the exact text, a muted privacy line with a lock icon (once; gone from the status bar), [Начать запись] (primary), [Загрузить расписание] (to the schedule), [Импортировать запись] (import panel). While the start or import panel is open and the list is empty, the block is not drawn (no second «Начать запись»); focus goes into the panel and back to the first button on close. The head buttons show only for a loaded non-empty list (`headActionsVisible`).
- «Фоновая обработка»: folded by default, hidden with no jobs, state kept across polls. Count = unfinished + failed jobs (no number when 0). Failures: a text button «С ошибкой: N — показать» unfolds the block and the inner «Завершённые» (`backgroundFailedText`, `backgroundShowFailed`); failures also stay on their recording row. Own header markup identical to `CollapsibleList` (which has no way to omit the count and is outside the allowed files).
- Row hints are gone: text buttons «Повторить» (keeps work-specific aria-label/tooltip, busy/disabled) and «Открыть подключения» (not duplicated with the main button); the round arrow is removed.
- Subject page: empty folders fold into «Пусто: Семинары [+] Лабораторные [+] …» (gap-only, each «+» a Tab-reachable button, results/errors name the folder). A folder created on this visit and «новое» folders stay sections with «Пока пусто» and scroll into view. «Ближайшее» rows open their assignment (`onOpenAssignment` from App; spans inside a button).
- Export paragraph of the knowledge base → tooltip + `aria-describedby` of the export button (a separate text when export is disabled).
- `Dialog`: action first, «Отмена» last (right-aligned); initial focus on «Отмена»; if it is disabled, focus goes to the dialog itself. No submit button, Enter cannot fire the action.

## Decisions to confirm
- Both head buttons («Начать запись», «Импортировать запись») are hidden on an empty list (the empty block has all three).
- Count «Фоновая обработка (N)» counts unfinished + failed only.
- Failure line wording «С ошибкой: N — показать» (glossary forbids «задачи»).
- «Загрузить расписание» kept as in the AC although the schedule review uses «Импортировать расписание» (ux-expert: pick one verb product-wide).
- The knowledge-base export tooltip is temporary (ADR-0034 §3 moves export to «⋯» of the tab «Пары»).

## Follow-ups
- `RemoveSubjectDialog` in `pages/SubjectRemoval.tsx` still has the old order (Отмена left, red action right); outside the allowed files. Needs the swap, keeping «Отмена» focused.
- Glossary: one verb for the schedule («Загрузить» vs «Импортировать»).
- `backgroundFailedText` treats an open «Завершённые» as showing every failed row though the list is cut at 8 (failed first sort or pass the limit).
- Focus drops to `body` if the first recording arrives right after the panel closed in the empty state (rare).
- Focus on a folder's «+» is lost when the first file moves it out of the «Пусто» line (TASK-353).
- Many page-wiring tests are source-text regexes (no DOM harness).

## Changed existing tests
`recordingrow.test.mjs` (round-arrow tests rewritten for text buttons; `retryHints` test deleted with its function; page-source checks: no group-title h2/`tasksEmptyText`/`EmptyState`, own background header, `rec-empty` with `!panelOpen`; `.retry` asserts -> `rowRetry`), `lecturetext.test.mjs` (hint is the plain error text), `library.test.mjs` and `subjectsHome341.test.mjs` («В этой папке пока нет файлов»/«0 файлов» -> «Пусто: …» line), new `quiet347.test.mjs`.

## Review
Reviewer: changes requested (failed jobs folded twice, count, wording) -> fix round -> re-review approve (one a11y minor fixed). ux-expert: «доработать» (panels far from the button, plain failure line with «задача», created folder vanishing, count, tooltip texts, small markup items) -> all fixed except the verb «Загрузить/Импортировать расписание» (AC wording). Security-auditor: not run — view-only change, no binding or trust boundary touched.
