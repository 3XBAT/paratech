# TASK-346 — «Задания» and «Журнал»: one primary action, grouped list, reasons in place

## Done
- Frontend only (view). «Добавить задание» is primary in the list head; first run: «Задания находятся сами в записи пары после конспекта.» + «Добавить задание вручную».
- List grouped «Просрочено · На этой неделе · Позже · Без срока» (`groupAssignments` in homeworkView.ts), empty groups not drawn, each group has its own «Показать ещё» window (`GroupedRows`, state `lists` keyed by group). «Выполненные» stays flat; in «Все» the done ones follow in a last group «Выполнено»; after marking done there the focus stays on the row's check (`rowsChangingGroup`, `focusKeepTarget`, `useDoneFocus.rowsMoving`).
- Assignment page: «Выполнено» visible; «Изменить», «Поправить предмет и преподавателя», «Удалить» (last) in «⋯» (RowMenu, unchanged); TASK-338 delete flow unchanged; focus returns to «⋯» after «Отмена»/«Вернуть». «Сохранить ▾» (Файл .txt / Word) replaces two buttons; «Отправить преподавателю…» (with the dirty-edit reason). «Отправить ещё раз…» is secondary; «Письмо дошло» has one line instead of the core hint.
- Journal: no «Обновить»; load error with «Повторить» (keeps the list); «Нельзя отменить: <причина>» incl. a proposal and a generic reason; the undo outcome in the row (under the filters if the row is filtered out); status not repeated in the reason; «Период» and «Только отменяемые» visible, the rest under «Ещё фильтры» (open/with a count when set); empty state with active filters has «Сбросить фильтры».
- New `src/pages/HomeworkMenus.tsx` (GroupedRows, first run, DeliveryRow, head menu, SaveMenu); new test `test/homeworkJournal346.test.mjs`.

## Decisions to confirm
- «На этой неделе» = today through today + 6 days (next 7 days); day 7 is «Позже».
- Done ones: «Открытые» none, «Выполненные» flat, «Все»: a last group «Выполнено» (ADR lists four groups only).
- Unknown-outcome email row: the AC line was extended to «Почта не подтвердила доставку. Если преподаватель письмо получил — отметьте «Письмо дошло». Повторная отправка может прийти вторым письмом.» and the core hint is not shown next to it (it said the same three ways).
- Proposal in the journal: «Нельзя отменить: это предложение, решение за вами».
- «Сохранить ▾» kept as the AC words it.

## Files outside the allowed list
`src/pages/HomeworkMenus.tsx` (new), `tsconfig.test.json` (one `files` line so tests load it), `src/assignmentDoneView.ts` and `src/pages/useDoneFocus.ts` (one helper each, focus after a mark in «Все»).

## Follow-ups
- Low: in «Все», a row marked done into «Выполнено» that already has 8+ rows may land past that group's window, then the focus falls to the page.
- Low (older): journal `load()` answers are not matched to the newest request (retry/undo while the filter changes).
- Low: focus lost after «Сбросить фильтры» in the journal.
- Nit: gap above the first group is 16 px instead of 24.
- UX expert's open items: «Сохранить ▾» next to «Сохранить правку» (consider «Сохранить как ▾»); two items that both fix the subject («Изменить» vs «Поправить предмет и преподавателя»); PROPOSED line «Нужно ваше решение»; overdue order/colour; email filter «Все/Активные» vs state «Все/Открытые» naming.

## Changed existing tests
Expected strings or source-scan targets only: assignmentdone316 (head scan, `changing`/`marking` props), homeworkdeletewarn («Удалить» via the head menu, status text «Удаление…», focus on «⋯» instead of autoFocus), homeworkedit, homework (Word label, email-row scan reads HomeworkMenus.tsx), journal («Только отменяемые», new reason texts; proposal still matches /Нельзя отменить/).

## Review
Reviewer: changes requested (focus in «Все», proposal refusal) -> fixed; re-review approve. ux-expert: «доработать» (5 must-fix: per-group windows, email row text, journal empty with filters, focus, send disabled reason) -> fixed. Security-auditor: not run — view-only change, no logic beyond view state, no trust boundary.
