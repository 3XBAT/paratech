# TASK-346: «Задания» and «Журнал» — one primary action, grouped list, reasons in place

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-344
- **PRD sections / AC:** ADR-0034 §2; audit А-6.4, 6.5, 6.7, 6.9, 6.10, 6.11, 11.2, 11.3, 11.5, 0.10 (journal)
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-346

## Goal
The assignment list and page get one clear primary action and fewer equal buttons; the journal explains refusals where
they happen.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{HomeworkPage,HomeworkDialogs,AssignmentFilter,JournalPage,JournalParts}.tsx`,
  `src/{homeworkView,journalView,listView}.ts`, `src/ui/RowMenu.tsx` (reuse only), `src/styles.css`, tests

## Out of scope
- One entry «Сделать задание» (TASK-362), breadcrumbs (TASK-350), core changes.

## Acceptance criteria
- [ ] «Добавить задание» is the primary button of the list; the first-run empty state has it too («Задания находятся
      сами в записи пары после конспекта. [Добавить задание вручную]»).
- [ ] The list is grouped «Просрочено · На этой неделе · Позже · Без срока» (done ones keep TASK-316 behaviour); groups
      with no rows are not drawn.
- [ ] Assignment page head: «Выполнено» stays visible; «Изменить», «Поправить предмет и преподавателя» and «Удалить» go
      into «⋯» (Удалить last, the TASK-338 warning unchanged).
- [ ] Draft buttons: «Сохранить в файл» and «Сохранить в Word (.docx)» → one «Сохранить ▾» (Файл .txt / Word);
      «Отправить по почте…» → «Отправить преподавателю…».
- [ ] «Отправить ещё раз…» is secondary, not danger; «Письмо дошло» has the line «Почта не подтвердила доставку. Если
      преподаватель письмо получил — отметьте.».
- [ ] Journal: no «Обновить»; an error shows «Повторить» next to it; «Отменить нельзя» is a text with the reason
      («Нельзя отменить: письмо уже отправлено»), a generic reason when the core gives none; «Действие отменено» shows in
      the row; only «Период» and «Только отменяемые» are visible, the rest under «Ещё фильтры».
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
