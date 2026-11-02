# TASK-316: «Выполнено» in the assignments UI

## Done
- Row of the list (`AssignmentRowView`, `pages/AssignmentDoneParts.tsx`): round check button in the Row lead (empty ring / filled disc, action-based title and aria-label), done rows muted (struck in «Все»), «Выполнено 28 окт.» in the meta. The red «Удалить» moved into the «⋯» menu (`RowMenu`, hint on the disabled item); visible actions are «Открыть» and «⋯». Inline «Сохранение…»/«Удаление…» in the row.
- State switch «Открытые / Выполненные / Все» (`AssignmentStateSwitch`), default open, server filter through `listAssignments(state)`, stale answers dropped (`newestOnly`). The email filter resets on a switch and is hidden in «Выполненные»; «Сбросить фильтр» on the empty line. Empty states with buttons («Показать открытые»/«Показать выполненные», «Пока ничего не отмечено»).
- Assignment page: «Отметить выполненным» / «Вернуть в работу» before «Изменить»/«Удалить», `DoneStamp` «Выполнено 28 окт.»; edit and delete stay; shown version is kept after a mark.
- «Сегодня» → «Горит»: check button beside the open button (no nested interactives), row leaves, re-read; `homeworkText` says «… · выполнено» when `homeworkDone === homework`.
- Toast (`ui/Toast.tsx`, new): one at a time, only with «Отменить» (`doneSummary.undo`), inverse surface, anchored to the content column (`left:64px; right:var(--ag-w)`), names the assignment (single) or counts, 10 s pause on hover/focus/blur/hidden, Ctrl+Z only when visible and not in a field.
- `DoneRunner` (`src/doneRunner.ts`, React-free) + `useDoneRunner`: flight guard released before the refresh so «Отменить» is never swallowed; undo sends exactly the returned `priorDone`/`prior`.
- Errors (codes `conflict`, not `busy`): not_found/deleted -> «Задание уже удалено» + «Закрыть» and re-read; mark conflict -> «Задание изменилось, попробуйте ещё раз» + «Повторить» (a mark has no version check, retry is valid); undo conflict -> explanation + «Открыть задание» (page: text pointing at the button above); failed/unavailable/invalid -> text + «Повторить».
- Focus: after a row leaves it goes to the next row's check (only if focus was on a leaving check or lost); after the toast closes to the affected check or the page region (`aria-labelledby` on the h1).
- Merged with TASK-338 (delete with stop) by hand: its warning, «Открыть отправку», stopped line and mail landing live in the row via props; focus after «Отмена» returns to the «⋯» button; mark and delete cannot overlap during the impact check or the open warning.
- Agent tool: TASK-320.

## Decisions to confirm
- The page button reads «Отметить выполненным» (not «Выполнено» as in the task text): a check-icon button labelled «Выполнено» reads as a status (UX review). The done stamp keeps «Выполнено 28 окт.».
- The row check is a button (not a checkbox) with an action-based name; «Вернуть в работу» is its done-state tooltip and name.
- Not a visible «Вернуть в работу» text button in done rows: the same round button, per the supervisor's «quiet checkbox» direction.
- TASK-316 text said «busy»/«Повторить» for every conflict: superseded by the TASK-315 decision (see above).

## Follow-ups (UX review, not done)
- Two filter rows still both have «Все» (email filter is not renamed); single-item «⋯» menu (add «Изменить»); unify the undo pattern (inline «Вернуть» of a deletion vs the toast); page-level chip for done; delayed busy text; `AssignmentRowView` into its own file; the list's `askRemove` in flight can still set `confirmId` after a state switch (row-present check after the await); refusal/busy state is shared per list (one busy id).
- `focusSpot`/`focusRowMenu` live in `assignmentDoneView.ts` because `useDoneFocus.ts` is not in `tsconfig.test.json`.
- The page flows have source-pattern tests only (no DOM harness).

## Changed existing tests
- `test/lesson.test.mjs`: `lessonText(a)` counted in `HomeworkPage.tsx` twice -> once there and once in `AssignmentDoneParts.tsx`.
- `test/homeworkdeletewarn.test.mjs` (TASK-338): the list «Удалить» is now `onRemove` of the row (menu item), disabled state through `removeLock`/`busyText`, focus after «Отмена» through `focusRowMenu`; page assertions kept. No assertion removed.
- `tsconfig.test.json`: new compiled files.

## Security
Security-auditor (opus): no findings (titles only as text, undo from the call's own result, no action without a click). Reviewer: first review 5 medium (undo lost under guard, page conflict text, version jump, focus, runner tests) fixed; re-review 2 medium (focus stealing, Ctrl+Z on a hidden page) fixed; merge review approve with 1 medium (done/delete overlap) fixed. The UX expert agent type was not registered in this session: a general-purpose opus run with the ux-expert definition did the pass; must-fix items done.
