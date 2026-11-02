# TASK-345: «Сегодня» — quieter and clearer

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-344
- **PRD sections / AC:** ADR-0034 §2; audit А-0.10, 0.12, 1.2 (part), 1.5, 1.6, 1.7, 2.7
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-345

## Goal
Remove what competes with the day on «Сегодня» and say plainly what needs the student.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{TodayPage,TodayParts}.tsx`, `src/todayView.ts`, `src/styles.css` (today rules), tests

## Out of scope
- «Записи и задания» button and the `all` mode (TASK-348), clickable lesson rows (TASK-352), «Спросить paratech» chips
  moving to the panel (TASK-355), the checklist (TASK-357), core changes of the today screen.

## Acceptance criteria
- [ ] No «Обновить» button; a load error shows «Не удалось загрузить день. [Повторить]» in place.
- [ ] «Ждёт вашего «Да»» → «Нужно ваше решение»; «Горит на 7 дней» → «Сроки на неделе»; an urgent chip has the clock icon
      and a word («сегодня», «завтра»), not only colour.
- [ ] «Сообщения paratech» and «Готово за день» become one block «Итоги дня» at the end of the left column; empty → hidden.
- [ ] «Спросить paratech» is hidden while there are no chips.
- [ ] The status bar no longer shows «Звук обрабатывается только на этом компьютере»; it shows only the queue and the
      nearest deadline, and is hidden when both are empty.
- [ ] `SpeechComponentNotice` shows on «Сегодня» when the speech module is missing (same component as «Записи»).
- [ ] Empty blocks of an empty day show one line each, no «пока нет» walls (until TASK-357).
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
