# TASK-347: «Записи», «Предметы» and dialogs — less noise, Windows button order

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-344
- **PRD sections / AC:** ADR-0034 §2; audit А-0.11, 0.12, 2.2, 2.3, 2.4, 4.2, 4.6, 9.3
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-347

## Goal
«Записи» shows recordings first with one way to start; the subject page stops listing empty folders; dialogs follow
the Windows order.

## Allowed to change
- `paratech/desktop/frontend/src/pages/{RecordingsPage,RecordingRow,SubjectsPage,SubjectsParts,KnowledgeBasePage}.tsx`,
  `src/{recordingRowView,activityView,libraryView}.ts`, `src/ui/controls.tsx` (Dialog only), `src/App.tsx` (only passing
  `onOpenAssignment` to «Предметы»), `src/styles.css`, tests

## Out of scope
- «Записи» in the rail (TASK-348), subject tabs (TASK-353), recording checkbox behaviour (А-2.5, later).

## Acceptance criteria
- [ ] Empty «Записи»: «Здесь будут записи пар. Записать можно сейчас — кнопкой «Начать запись», или загрузить
      расписание, и paratech сам предложит запись в начале пары.» with [Начать запись] (primary), [Загрузить
      расписание], [Импортировать запись]; the head primary is hidden while the list is empty.
- [ ] «Задачи» → «Фоновая обработка», folded by default with a count; failures still show on their recording row.
- [ ] Row hints «Нажмите круглую стрелку…» / «Проверьте подключения (меню «⋯»)» become text buttons «Повторить» /
      «Открыть подключения».
- [ ] The status bar line «Звук обрабатывается только на этом компьютере» goes (the same text stays once in the empty state).
- [ ] Subject page: empty folders fold into one line «Пусто: Семинары, Лабораторные, Билеты»; the «+» of each folder
      stays reachable (in the line or on hover/focus of the folded name).
- [ ] «Ближайшее» rows open their assignment.
- [ ] The export paragraph of a knowledge-base subject becomes the tooltip of the export button.
- [ ] `Dialog`: confirm button left, «Отмена» right; initial focus stays on the safe button; all dialog tests updated.
- [ ] UX check of coordinator-round.md 3a; `npm run test` and `npm run build` pass.
