# TASK-348: Rail with labels, «Записи» as a section, «Записать пару» on «Сегодня»

- **Agent:** frontend (sonnet)
- **Label:** frontend
- **Depends on:** TASK-345, TASK-347
- **PRD sections / AC:** ADR-0034 §1, §2; audit А-0.1, 0.2, 1.1, 11.1, 14.1
- **Агент:** только UI: pure view state
- **Branch:** ralph/<date>-task-348

## Goal
Every section is visible and named in the rail, recordings have their own place, and the main action of the day is one
click away.

## Allowed to change
- `paratech/desktop/frontend/src/{nav.ts,App.tsx,navHistory.ts,useNavHistory.ts,agentView.ts (screens map only)}`,
  `src/ui/{Rail.tsx,Icon.tsx}`, `src/pages/{TodayPage,TodayParts,RecordingsPage}.tsx`, `src/todayView.ts`,
  `src/styles.css` (rail and today rules), tests

## Out of scope
- Busy pill (TASK-349), breadcrumbs (TASK-350).

## Acceptance criteria
- [ ] The rail shows icon + label under it (72 px): top «Сегодня, Записи, Предметы, Задания, Расписание», bottom «Журнал,
      Настройки»; keyboard and aria as today; labels do not wrap at 100–200 % scale.
- [ ] New page `recordings` («Записи») shows the current RecordingsPage; the `all` mode and the «Записи и задания» and
      «К сегодняшнему дню» buttons of «Сегодня» are gone; «Проблема с записью» opens «Записи».
- [ ] «Сегодня» has the primary «Записать пару»: it opens «Записи» with the start panel open (one-shot state like
      `scheduleReview`, not part of the route); while a recording runs it reads «● Идёт запись 00:41 · Остановить» and
      stops it (same binding as the recordings page).
- [ ] Alt+← / mouse buttons and the rail «click again → first level» work for the new section.
- [ ] The agent panel context for `recordings` is «Записи».
- [ ] UX check of coordinator-round.md 3a; ux-expert «ок»; `npm run test` and `npm run build` pass.
