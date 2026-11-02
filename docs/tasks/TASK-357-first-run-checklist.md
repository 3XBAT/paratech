# TASK-357: «Начало работы» checklist on «Сегодня»

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-348, TASK-355
- **PRD sections / AC:** ADR-0034 §2; audit А-1.9, 2.7, 12.3, «Итог 2» A; ux-guidelines §5 (no carousel)
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-357

## Goal
A new student sees what to do first instead of five empty blocks.

## Allowed to change
- new `src/pages/StartChecklist.tsx`, new `src/startChecklistView.ts`, `src/pages/{TodayPage,TodayParts}.tsx`,
  `src/styles.css`, tests

## Out of scope
- New bindings or core facts. If a step's fact is not available from the existing bindings, leave the step out and
  write it in the progress file.

## Acceptance criteria
- [ ] Steps, each one button that leads straight to the action, ticked by fact: «Проверить звук» (Диагностика →
      проверка), «Установить модуль расшифровки» (only when missing; progress in the step), «Загрузить расписание»,
      «Записать первую пару» (opens «Записи» with the start panel), «Указать почту преподавателя» (shows after the first
      assignment; opens the subject's teachers).
- [ ] No Hub key: one line above the list «Конспекты недоступны без ключа доступа. Запись и расшифровка работают.
      [Ввести ключ]».
- [ ] Shown while fewer than 3 steps are done; «Скрыть» hides it for good (local UI preference, try/catch); it folds.
- [ ] Replaces the empty blocks of an empty day; a normal day is unchanged.
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.
